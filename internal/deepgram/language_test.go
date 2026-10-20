// SPDX-License-Identifier: Apache-2.0
package deepgram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/FreshLabDev/voicy/internal/metrics"
)

// detectionLanguages is the list on developers.deepgram.com/docs/language-detection
// as read on 2026-10-18. HintLanguages is defined against it: a language
// detection knows needs no hint, and naming it would only cost a second run when
// the audio is not in it.
var detectionLanguages = strings.Fields(`bg ca cs da de de-CH el en es et fi fr hi hu id it ja ko lt lv ms nl nl-BE
	no pl pt ro ru sk sv th tr uk vi zh`)

func TestHintLanguagesAreTheOnesDetectionLacks(t *testing.T) {
	if len(detectionLanguages) != 35 {
		t.Fatalf("the detection list has %d entries, Deepgram documents 35", len(detectionLanguages))
	}
	seen := map[string]bool{}
	for _, l := range HintLanguages {
		if seen[l.Code] {
			t.Errorf("%s is listed twice", l.Code)
		}
		seen[l.Code] = true
		if l.Code != strings.ToLower(l.Code) || strings.ContainsAny(l.Code, "-_") {
			t.Errorf("%q is not a bare lower-case code", l.Code)
		}
		if strings.TrimSpace(l.Name) == "" {
			t.Errorf("%s has no name for the picker", l.Code)
		}
		for _, d := range detectionLanguages {
			if l.Code == d {
				t.Errorf("%s is in Deepgram's detection list and must not be hinted", l.Code)
			}
		}
	}
	// The three the bot was asked to fix, and the ones the live probe showed
	// being mistaken for French, Polish, Turkish and Slovak.
	for _, code := range []string{"ar", "fa", "be", "he", "kk", "bn", "hr", "ur"} {
		if !seen[code] {
			t.Errorf("%s must be hinted", code)
		}
	}
	// Not supported by nova-3 at all, so there is nothing to hint.
	for _, code := range []string{"uz", "km", "my"} {
		if seen[code] {
			t.Errorf("%s is not a nova-3 language", code)
		}
	}
}

func TestHintFor(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ar", "ar"},
		{"AR", "ar"},
		{" ar-EG ", "ar"},
		{"fa_IR", "fa"},
		{"be", "be"},
		{"iw", "he"},
		{"fil", "tl"},
		// Detection knows these, so no hint.
		{"en", ""},
		{"ru", ""},
		{"uk", ""},
		{"zh-Hans", ""},
		// Nova-3 does not transcribe these.
		{"uz", ""},
		{"km", ""},
		{"my", ""},
		{"", ""},
		{"klingon", ""},
		{"-", ""},
	} {
		if got := HintFor(tc.in); got != tc.want {
			t.Errorf("HintFor(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNameOf(t *testing.T) {
	if got := NameOf("fa"); got != "فارسی" {
		t.Errorf("NameOf(fa) = %q", got)
	}
	if got := NameOf("zz"); got != "zz" {
		t.Errorf("an unknown code names itself, got %q", got)
	}
}

func TestRestQueryNamesTheLanguage(t *testing.T) {
	named := DefaultOptions
	named.Language = "ar"
	q, err := url.ParseQuery(restQuery(named))
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("language") != "ar" {
		t.Fatalf("query %v does not name the language", q)
	}
	// Deepgram lets detect_language override language, so sending both would
	// silently undo the hint.
	if _, ok := q["detect_language"]; ok {
		t.Fatalf("a named language must not be sent with detect_language: %v", q)
	}
	if q.Get("model") != "nova-3" || q.Get("mip_opt_out") != "true" {
		t.Fatalf("the rest of the query changed: %v", q)
	}
}

func TestRestQueryDetectsWhenNoLanguageIsNamed(t *testing.T) {
	q, err := url.ParseQuery(restQuery(DefaultOptions))
	if err != nil {
		t.Fatal(err)
	}
	if q.Get("detect_language") != "true" {
		t.Fatalf("an unnamed language must be detected: %v", q)
	}
	if _, ok := q["language"]; ok {
		t.Fatalf("an unnamed language must not send language: %v", q)
	}
}

// The language is a request parameter, so it reaches the wire on the file
// upload too and not only in restQuery.
func TestListenFileSendsTheNamedLanguage(t *testing.T) {
	srv, got := listenServer(t, reply("نص", 0.95, 4))
	c := newTestClient(srv)
	opts := DefaultOptions
	opts.Language = "fa"
	if _, err := c.ListenFile(context.Background(), audioFile(t, "OGG"), "", opts); err != nil {
		t.Fatal(err)
	}
	q := got.query(t, 0)
	if q.Get("language") != "fa" || q.Get("detect_language") != "" {
		t.Fatalf("query = %v", q)
	}
}

// --- fallback ---------------------------------------------------------------

func reply(text string, confidence, duration float64) string {
	return fmt.Sprintf(`{"metadata":{"request_id":"r","duration":%v},"results":{"channels":[{"detected_language":"","alternatives":[{"transcript":%q,"confidence":%v}]}]}}`,
		duration, text, confidence)
}

func detected(lang, text string, confidence, duration float64) string {
	return fmt.Sprintf(`{"metadata":{"request_id":"r","duration":%v},"results":{"channels":[{"detected_language":%q,"alternatives":[{"transcript":%q,"confidence":%v}]}]}}`,
		duration, lang, text, confidence)
}

// seen records the query of every request, in order.
type seen struct {
	mu      sync.Mutex
	queries []string
}

func (s *seen) add(q string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
}

func (s *seen) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queries)
}

func (s *seen) query(t *testing.T, i int) url.Values {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if i >= len(s.queries) {
		t.Fatalf("request %d never happened, %d did", i, len(s.queries))
	}
	q, err := url.ParseQuery(s.queries[i])
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// listenServer answers the n-th request with the n-th body; a body that starts
// with "HTTP " is an error status instead ("HTTP 500").
func listenServer(t *testing.T, bodies ...string) (*httptest.Server, *seen) {
	t.Helper()
	got := &seen{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := got.count()
		got.add(r.URL.RawQuery)
		if n >= len(bodies) {
			t.Errorf("request %d has no scripted answer", n+1)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if status, ok := strings.CutPrefix(bodies[n], "HTTP "); ok {
			code, _ := strconv.Atoi(status)
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"err_msg":"scripted"}`))
			return
		}
		_, _ = w.Write([]byte(bodies[n]))
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func newTestClient(srv *httptest.Server) *Client {
	c := New("k")
	c.SetRESTBase(srv.URL)
	c.SetHTTP(srv.Client())
	return c
}

func audioSecondsMetric(t *testing.T) int {
	t.Helper()
	var sb strings.Builder
	metrics.Write(&sb)
	for _, line := range strings.Split(sb.String(), "\n") {
		if v, ok := strings.CutPrefix(line, "voicy_audio_seconds_total "); ok {
			n, err := strconv.Atoi(v)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatal("voicy_audio_seconds_total is not exposed")
	return 0
}

func fallbacksMetric(t *testing.T) int {
	t.Helper()
	var sb strings.Builder
	metrics.Write(&sb)
	for _, line := range strings.Split(sb.String(), "\n") {
		if v, ok := strings.CutPrefix(line, "voicy_language_fallbacks_total "); ok {
			n, err := strconv.Atoi(v)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatal("voicy_language_fallbacks_total is not exposed")
	return 0
}

func arabic() Options {
	o := DefaultOptions
	o.Language = "ar"
	o.AudioSeconds = 9
	return o
}

func TestTranscribeKeepsAConfidentNamedRun(t *testing.T) {
	srv, got := listenServer(t, reply("مرحبا بك", 0.95, 9.2))
	c := newTestClient(srv)
	audioBefore, fallbacksBefore := audioSecondsMetric(t), fallbacksMetric(t)

	res, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", arabic())
	if err != nil {
		t.Fatal(err)
	}
	if got.count() != 1 {
		t.Fatalf("a confident named run must not be repeated, requests = %d", got.count())
	}
	if res.Text != "مرحبا بك" {
		t.Fatalf("text = %q", res.Text)
	}
	// Deepgram does not report a language it was told, so the result does.
	if res.Language != "ar" {
		t.Fatalf("language = %q, want the named one", res.Language)
	}
	if res.Billed != 9.2 {
		t.Fatalf("billed = %v, want one run", res.Billed)
	}
	if d := audioSecondsMetric(t) - audioBefore; d != 9 {
		t.Fatalf("audio seconds counted = %d, want 9", d)
	}
	if d := fallbacksMetric(t) - fallbacksBefore; d != 0 {
		t.Fatalf("fallbacks counted = %d, want 0", d)
	}
}

func TestTranscribeWithoutALanguageDetectsOnce(t *testing.T) {
	srv, got := listenServer(t, detected("en", "hello there", 0.99, 5))
	c := newTestClient(srv)
	opts := DefaultOptions
	opts.AudioSeconds = 5
	audioBefore := audioSecondsMetric(t)

	res, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	q := got.query(t, 0)
	if got.count() != 1 || q.Get("detect_language") != "true" || q.Get("language") != "" {
		t.Fatalf("requests = %d, query = %v", got.count(), q)
	}
	if res.Language != "en" || res.Text != "hello there" {
		t.Fatalf("result = %+v", res)
	}
	if res.Billed != 5 {
		t.Fatalf("billed = %v", res.Billed)
	}
	if d := audioSecondsMetric(t) - audioBefore; d != 5 {
		t.Fatalf("audio seconds counted = %d, want 5", d)
	}
}

// An Arabic speaker forwards an English voice message: the named run hears
// nothing, detection hears English, and the person gets English.
func TestTranscribeFallsBackWhenTheNamedRunIsEmpty(t *testing.T) {
	srv, got := listenServer(t,
		reply("", 0, 5.2),
		detected("en", "could you send me the address", 1.0, 5.2),
	)
	c := newTestClient(srv)
	opts := arabic()
	opts.AudioSeconds = 5
	audioBefore, fallbacksBefore := audioSecondsMetric(t), fallbacksMetric(t)

	res, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", opts)
	if err != nil {
		t.Fatal(err)
	}
	if got.count() != 2 {
		t.Fatalf("requests = %d, want the named run and the detecting run", got.count())
	}
	first, second := got.query(t, 0), got.query(t, 1)
	if first.Get("language") != "ar" || first.Get("detect_language") != "" {
		t.Fatalf("first request = %v", first)
	}
	if second.Get("detect_language") != "true" || second.Get("language") != "" {
		t.Fatalf("second request = %v", second)
	}
	// Everything else about the two requests is the same option set.
	for _, key := range []string{"model", "smart_format", "paragraphs", "mip_opt_out"} {
		if first.Get(key) != second.Get(key) {
			t.Errorf("%s differs between the runs: %q vs %q", key, first.Get(key), second.Get(key))
		}
	}
	if res.Text != "could you send me the address" || res.Language != "en" {
		t.Fatalf("result = %+v", res)
	}
	// Both runs were billed, and both are counted as a normal run would be.
	if res.Billed != 10.4 {
		t.Fatalf("billed = %v, want both runs", res.Billed)
	}
	if res.Duration != 5.2 {
		t.Fatalf("duration = %v, statistics must see the file once", res.Duration)
	}
	if d := audioSecondsMetric(t) - audioBefore; d != 10 {
		t.Fatalf("audio seconds counted = %d, want 10", d)
	}
	if d := fallbacksMetric(t) - fallbacksBefore; d != 1 {
		t.Fatalf("fallbacks counted = %d, want 1", d)
	}
}

func TestTranscribeFallsBackOnLowConfidenceAndTakesTheBetterRun(t *testing.T) {
	srv, got := listenServer(t,
		reply("ghgh sdf", 0.41, 6),
		detected("en", "this is clearly english", 0.97, 6),
	)
	c := newTestClient(srv)
	res, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", arabic())
	if err != nil {
		t.Fatal(err)
	}
	if got.count() != 2 || res.Text != "this is clearly english" || res.Language != "en" {
		t.Fatalf("requests = %d, result = %+v", got.count(), res)
	}
	if res.Confidence != 0.97 {
		t.Fatalf("confidence = %v, must belong to the transcript that was kept", res.Confidence)
	}
}

// The detector, asked about a language it does not know, answers confidently in
// Latin letters. A hesitant transcript in the right script is the better one.
func TestTranscribeKeepsAHesitantNamedRunOverAWeakGuess(t *testing.T) {
	srv, got := listenServer(t,
		reply("مرحبا كيف حالك", 0.62, 9),
		detected("fr", "Marhaban kaifa halek", 0.60, 9),
	)
	c := newTestClient(srv)
	res, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", arabic())
	if err != nil {
		t.Fatal(err)
	}
	if got.count() != 2 {
		t.Fatalf("requests = %d, a hesitant run is still checked", got.count())
	}
	if res.Text != "مرحبا كيف حالك" || res.Language != "ar" {
		t.Fatalf("result = %+v", res)
	}
	// The second run was paid for even though its text was thrown away.
	if res.Billed != 18 {
		t.Fatalf("billed = %v, want both runs", res.Billed)
	}
}

// A better score is not enough while it is still below the bar: that is the
// detector being wrong with more confidence than the named run has.
func TestTranscribeDoesNotTrustAWeakDetectionJustForBeingHigher(t *testing.T) {
	srv, _ := listenServer(t,
		reply("مرحبا", 0.50, 4),
		detected("id", "Marhaban", 0.80, 4),
	)
	c := newTestClient(srv)
	res, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", arabic())
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "مرحبا" {
		t.Fatalf("a detection under the bar replaced the named run: %+v", res)
	}
}

// When the named run heard nothing there is nothing to protect: the person gets
// what detection heard, as before the language was named.
func TestTranscribeFallsBackToWhateverDetectionHeardWhenTheNamedRunIsEmpty(t *testing.T) {
	srv, _ := listenServer(t,
		reply("", 0, 8),
		detected("fr", "Choukran jazilan", 0.575, 8),
	)
	c := newTestClient(srv)
	res, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", arabic())
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Choukran jazilan" || res.Language != "fr" {
		t.Fatalf("result = %+v", res)
	}
}

func TestTranscribeBothRunsEmptyIsAnEmptyTranscript(t *testing.T) {
	srv, _ := listenServer(t, reply("", 0, 3), detected("en", "", 0, 3))
	c := newTestClient(srv)
	res, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", arabic())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(res.Text) != "" {
		t.Fatalf("text = %q", res.Text)
	}
	if res.Billed != 6 {
		t.Fatalf("billed = %v, both runs hear the whole file", res.Billed)
	}
}

func TestTranscribeKeepsTheNamedRunWhenTheRepeatFails(t *testing.T) {
	srv, got := listenServer(t, reply("مرحبا", 0.55, 4), "HTTP 400")
	c := newTestClient(srv)
	res, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", arabic())
	if err != nil {
		t.Fatalf("a failed repeat must not fail a job that has a transcript: %v", err)
	}
	if got.count() != 2 || res.Text != "مرحبا" {
		t.Fatalf("requests = %d, result = %+v", got.count(), res)
	}
	// The failed request billed nothing; only the run that produced the text did.
	if res.Billed != 4 {
		t.Fatalf("billed = %v", res.Billed)
	}
}

func TestTranscribeFailsWhenTheRepeatFailsAndNothingWasHeard(t *testing.T) {
	srv, _ := listenServer(t, reply("", 0, 4), "HTTP 400")
	c := newTestClient(srv)
	if _, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", arabic()); err == nil {
		t.Fatal("an empty named run followed by a failed repeat is a failure, not 'no speech'")
	}
}

func TestTranscribeReportsAFailedNamedRun(t *testing.T) {
	srv, got := listenServer(t, "HTTP 400")
	c := newTestClient(srv)
	if _, err := c.Transcribe(context.Background(), audioFile(t, "OGG"), "", arabic()); err == nil {
		t.Fatal("expected the error")
	}
	if got.count() != 1 {
		t.Fatalf("requests = %d, a permanent error is not repeated with detection", got.count())
	}
}

func TestPickTranscript(t *testing.T) {
	named := func(text string, conf float64) Result { return Result{Text: text, Confidence: conf, Language: "ar"} }
	found := func(text string, conf float64) Result { return Result{Text: text, Confidence: conf, Language: "en"} }
	for _, tc := range []struct {
		name            string
		named, detected Result
		want            string
	}{
		{"detection heard nothing", named("a", 0.5), found("", 0), "a"},
		{"both empty", named("", 0), found("", 0), ""},
		{"named empty, detection weak", named("", 0), found("junk", 0.3), "junk"},
		{"named weak, detection strong", named("a", 0.5), found("b", 0.9), "b"},
		{"named weak, detection stronger but under the bar", named("a", 0.5), found("b", 0.8), "a"},
		{"named weak, detection at the bar", named("a", 0.5), found("b", MinHintConfidence), "b"},
		{"named weaker than the bar, detection not better", named("a", 0.84), found("b", 0.84), "a"},
	} {
		if got := pickTranscript(tc.named, tc.detected).Text; got != tc.want {
			t.Errorf("%s: kept %q, want %q", tc.name, got, tc.want)
		}
	}
}
