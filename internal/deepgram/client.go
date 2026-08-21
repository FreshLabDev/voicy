// SPDX-License-Identifier: Apache-2.0
package deepgram

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultREST = "https://api.deepgram.com"
	ListenPath  = "/v1/listen"
	maxResponse = 8 << 20
)

type Client struct {
	apiKey string
	rest   string
	http   *http.Client
}

func New(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		rest:   DefaultREST,
		http:   &http.Client{Timeout: 2 * time.Minute},
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
}

// DefaultOptions mirrors the option set used before per-user settings existed
// and therefore the settings.DefaultVariant cache rows.
var DefaultOptions = Options{SmartFormat: true, Paragraphs: true}

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
	q.Set("detect_language", "true")
	q.Set("mip_opt_out", "true")
	return q.Encode()
}

func boolParam(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (c *Client) ListenFile(ctx context.Context, audio []byte, contentType string, opts Options) (Result, error) {
	if len(audio) == 0 {
		return Result{}, fmt.Errorf("empty audio")
	}
	if contentType == "" {
		contentType = "audio/ogg"
	}
	// A zero Options is legitimate "everything off": substituting defaults
	// here would desync the request from the cache variant it is saved under.
	endpoint := c.rest + ListenPath + "?" + restQuery(opts)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(audio))
	if err != nil {
		return Result{}, redact(c.apiKey, err)
	}
	req.Header.Set("Authorization", "Token "+c.apiKey)
	req.Header.Set("Content-Type", contentType)
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, redact(c.apiKey, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return Result{}, err
	}
	if len(body) > maxResponse {
		return Result{}, fmt.Errorf("deepgram listen response exceeds %d bytes", maxResponse)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("deepgram listen HTTP %d", resp.StatusCode)
	}
	return ExtractPrerecorded(body)
}

func (c *Client) Transcribe(ctx context.Context, audio []byte, contentType string, opts Options) (Result, error) {
	return c.ListenFile(ctx, audio, contentType, opts)
}

func redact(key string, err error) error {
	if err == nil || key == "" {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(err.Error(), key, "***"))
}
