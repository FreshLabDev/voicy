// SPDX-License-Identifier: Apache-2.0
package deepgram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/FreshLabDev/voicy/internal/httpx"
	"github.com/FreshLabDev/voicy/internal/metrics"
)

const (
	DefaultREST = "https://api.deepgram.com"
	ListenPath  = "/v1/listen"
	maxResponse = 8 << 20

	// maxAttempts covers one transient Deepgram failure plus a retry. Production
	// has seen the same file answered with HTTP 408 twice in a row with no retry
	// at all, which surfaced to the user as a plain "could not transcribe".
	maxAttempts = 3

	// A request deadline has to scale with the audio: at MAX_MEDIA_DURATION=1h a
	// fixed two-minute client timeout cannot be met by any model.
	baseTimeout     = 90 * time.Second
	timeoutPerAudio = 5 // one second of budget per this many seconds of audio
	maxTimeout      = 10 * time.Minute
)

type Client struct {
	apiKey string
	rest   string
	http   *http.Client
}

func New(apiKey string) *Client {
	// No client-level timeout: every attempt carries its own context deadline,
	// sized from the audio length.
	return &Client{
		apiKey: apiKey,
		rest:   DefaultREST,
		http:   httpx.New(),
	}
}

func (c *Client) SetRESTBase(base string) { c.rest = strings.TrimRight(base, "/") }
func (c *Client) SetHTTP(h *http.Client) {
	if h != nil {
		c.http = h
	}
}

// Options are per-request transcription options derived from user settings.
type Options struct {
	SmartFormat     bool
	Paragraphs      bool
	FillerWords     bool
	ProfanityFilter bool
	Diarize         bool

	// Language names the spoken language, one of HintLanguages. Empty asks
	// Deepgram to detect it. It is part of the cache variant, so the bot derives
	// the variant from the same value it puts here.
	Language string

	// AudioSeconds sizes the request deadline and the audio-seconds counter. It
	// is a client-side hint only: restQuery never sends it, so it can never
	// change a cache variant.
	AudioSeconds int
}

// DefaultOptions mirrors the option set used before per-user settings existed
// and therefore the settings.DefaultVariant cache rows.
var DefaultOptions = Options{SmartFormat: true, Paragraphs: true}

// restQuery sends language=<code> when the language is named and
// detect_language=true otherwise. Deepgram lets detect_language override
// language, so the two are never sent together.
func restQuery(o Options) string {
	q := url.Values{}
	q.Set("model", "nova-3")
	q.Set("smart_format", boolParam(o.SmartFormat))
	q.Set("paragraphs", boolParam(o.Paragraphs))
	if o.FillerWords {
		q.Set("filler_words", "true")
	}
	if o.ProfanityFilter {
		q.Set("profanity_filter", "true")
	}
	if o.Diarize {
		q.Set("diarize_model", "latest")
	}
	if o.Language != "" {
		q.Set("language", o.Language)
	} else {
		q.Set("detect_language", "true")
	}
	q.Set("mip_opt_out", "true")
	return q.Encode()
}

func boolParam(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// APIError is a non-2xx answer from Deepgram, carrying the identifiers needed to
// chase a failure in Deepgram's own console.
type APIError struct {
	StatusCode int
	RequestID  string
	Detail     string
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	msg := "deepgram listen HTTP " + strconv.Itoa(e.StatusCode)
	if e.RequestID != "" {
		msg += " (request " + e.RequestID + ")"
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// retryable reports whether another attempt can plausibly succeed. 408 and 429
// are explicitly included: both are what a busy Deepgram returns for work that
// would have completed on a second try.
func retryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusRequestTimeout ||
			apiErr.StatusCode == http.StatusTooManyRequests ||
			apiErr.StatusCode >= 500
	}
	// Transport failures and deadlines of a single attempt are worth repeating;
	// a cancelled parent context is not.
	return !errors.Is(err, context.Canceled)
}

// requestTimeout scales the per-attempt deadline with the length of the audio.
func requestTimeout(audioSeconds int) time.Duration {
	if audioSeconds <= 0 {
		return baseTimeout
	}
	d := baseTimeout + time.Duration(audioSeconds/timeoutPerAudio)*time.Second
	if d > maxTimeout {
		return maxTimeout
	}
	return d
}

func backoff(attempt int) time.Duration {
	d := time.Second << (attempt - 1)
	if d > 8*time.Second {
		d = 8 * time.Second
	}
	half := int64(d / 2)
	return d - time.Duration(half) + time.Duration(rand.Int63n(2*half+1))
}

// ListenFile transcribes the audio at path. The file is reopened per attempt
// rather than buffered, so a retry can replay a body of any size without the
// whole recording sitting in memory.
func (c *Client) ListenFile(ctx context.Context, path string, contentType string, opts Options) (Result, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Result{}, fmt.Errorf("audio unavailable: %w", err)
	}
	if info.Size() == 0 {
		return Result{}, fmt.Errorf("empty audio")
	}
	if contentType == "" {
		contentType = "audio/ogg"
	}
	timeout := requestTimeout(opts.AudioSeconds)
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := c.listenOnce(ctx, path, info.Size(), contentType, opts, timeout)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if ctx.Err() != nil || attempt == maxAttempts || !retryable(err) {
			return Result{}, err
		}
		metrics.DeepgramRetries.Inc()
		delay := backoff(attempt)
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
			delay = apiErr.RetryAfter
		}
		if err := c.sleep(ctx, delay); err != nil {
			return Result{}, lastErr
		}
	}
	return Result{}, lastErr
}

func (c *Client) listenOnce(ctx context.Context, path string, size int64, contentType string, opts Options, timeout time.Duration) (Result, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	audio, err := os.Open(path)
	if err != nil {
		return Result{}, fmt.Errorf("open audio: %w", err)
	}
	defer audio.Close()
	// A zero Options is legitimate "everything off": substituting defaults
	// here would desync the request from the cache variant it is saved under.
	endpoint := c.rest + ListenPath + "?" + restQuery(opts)
	req, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, endpoint, audio)
	if err != nil {
		return Result{}, redact(c.apiKey, err)
	}
	// Deepgram needs a length; without it the body would be chunked.
	req.ContentLength = size
	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, redact(c.apiKey, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return Result{}, redact(c.apiKey, err)
	}
	if len(body) > maxResponse {
		return Result{}, fmt.Errorf("deepgram listen response exceeds %d bytes", maxResponse)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, apiError(resp, body)
	}
	return ExtractPrerecorded(body)
}

// apiError keeps the Deepgram request id and a bounded slice of the body. The
// body is Deepgram's own error text, never audio and never a transcript.
func apiError(resp *http.Response, body []byte) *APIError {
	out := &APIError{
		StatusCode: resp.StatusCode,
		RequestID:  resp.Header.Get("dg-request-id"),
	}
	if detail := strings.TrimSpace(string(body)); detail != "" {
		if len(detail) > 200 {
			detail = detail[:200]
		}
		out.Detail = strings.Join(strings.Fields(detail), " ")
	}
	if raw := resp.Header.Get("Retry-After"); raw != "" {
		if secs, err := strconv.Atoi(raw); err == nil && secs > 0 && secs <= 60 {
			out.RetryAfter = time.Duration(secs) * time.Second
		}
	}
	return out
}

func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// MinHintConfidence is how sure Deepgram has to be of a transcript made with a
// named language before Transcribe believes it.
//
// Calibrated on 2026-10-18 against nova-3 with synthetic speech, because no
// recorded voice messages were at hand. Audio that was in the named language
// came back at 0.86 to 1.0 (Arabic, Hebrew, Kazakh, Bengali, Croatian; clean,
// pink noise and 8 kHz telephone band alike). Audio in another language came
// back empty at 0.0, English and Russian read as Arabic alike. The detector's
// wrong guesses on those languages scored 0.0 to 0.77. The threshold sits under
// every correct run and over every wrong guess, and it errs high: a needless
// second request costs seconds of audio, a believed wrong transcript costs the
// person the message.
const MinHintConfidence = 0.85

// Transcribe turns audio into text. With opts.Language set it asks for that
// language first and, when the answer is empty or not confident enough, runs the
// same file again with automatic detection and keeps the better transcript.
// That covers a person whose language Deepgram cannot detect forwarding a
// message in a language it can: the named run hears nothing, the detecting run
// hears English.
//
// Every request counts toward the audio-seconds metric, and Result.Billed is the
// total Deepgram billed, so the daily allowance sees the second run too.
func (c *Client) Transcribe(ctx context.Context, path string, contentType string, opts Options) (Result, error) {
	metrics.AudioSeconds.Add(int64(opts.AudioSeconds))
	first, err := c.ListenFile(ctx, path, contentType, opts)
	if err != nil {
		return Result{}, err
	}
	first.Billed = first.Duration
	if opts.Language == "" {
		return first, nil
	}
	// Deepgram does not report a language it was told; the person reads this in
	// the metadata line and the statistics card.
	first.Language = opts.Language
	if !distrusted(first) {
		return first, nil
	}

	metrics.LanguageFallbacks.Inc()
	metrics.AudioSeconds.Add(int64(opts.AudioSeconds))
	detect := opts
	detect.Language = ""
	second, err := c.ListenFile(ctx, path, contentType, detect)
	if err != nil {
		// What the named run heard is better than failing a job whose audio was
		// already paid for, but only when it heard something.
		if ctx.Err() == nil && strings.TrimSpace(first.Text) != "" {
			return first, nil
		}
		return Result{}, err
	}
	out := pickTranscript(first, second)
	out.Billed = first.Duration + second.Duration
	return out, nil
}

// distrusted reports whether a transcript made with a named language should be
// checked against automatic detection.
func distrusted(r Result) bool {
	return strings.TrimSpace(r.Text) == "" || r.Confidence < MinHintConfidence
}

// pickTranscript chooses between the run that named the language and the run
// that detected it. When the named run produced text, detection replaces it only
// if detection is itself confident and more so: on a language it does not know
// the detector guesses wrong and writes the speech out in Latin letters, and
// that must not win against a hesitant transcript in the right script. When the
// named run heard nothing there is nothing to protect, so the person gets what
// detection heard, which is what they were given before the language was named.
func pickTranscript(named, detected Result) Result {
	if strings.TrimSpace(detected.Text) == "" {
		return named
	}
	if strings.TrimSpace(named.Text) != "" &&
		(detected.Confidence < MinHintConfidence || detected.Confidence <= named.Confidence) {
		return named
	}
	return detected
}

func redact(key string, err error) error {
	if err == nil || key == "" {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), key, "***"))
}
