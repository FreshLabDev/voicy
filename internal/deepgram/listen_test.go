// SPDX-License-Identifier: Apache-2.0
package deepgram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListenFilePostsTokenAndBinary(t *testing.T) {
	var gotAuth, gotPath, gotCT string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"metadata":{"request_id":"r1","duration":1},"results":{"channels":[{"alternatives":[{"transcript":"rest text","confidence":1}]}]}}`))
	}))
	t.Cleanup(srv.Close)
	c := New("listen-secret")
	c.SetRESTBase(srv.URL)
	c.SetHTTP(srv.Client())
	got, err := c.ListenFile(context.Background(), []byte("OGG"), "audio/ogg", DefaultOptions)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Token listen-secret" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotPath != "/v1/listen" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotCT != "audio/ogg" || string(gotBody) != "OGG" {
		t.Fatalf("ct=%s body=%q", gotCT, gotBody)
	}
	if got.Text != "rest text" {
		t.Fatalf("text = %q", got.Text)
	}
	q := rquery(c)
	if !strings.Contains(q, "detect_language") {
		t.Fatal("rest query should detect language")
	}
	if !strings.Contains(q, "paragraphs=true") {
		t.Fatal("rest query should request paragraphs")
	}
	if strings.Contains(q, "language=multi") {
		t.Fatal("rest query must keep detect_language, not language=multi")
	}
}

func TestTranscribeUsesRESTListen(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"metadata":{"request_id":"r2","duration":1},"results":{"channels":[{"alternatives":[{"transcript":"via transcribe"}]}]}}`))
	}))
	t.Cleanup(srv.Close)
	c := New("listen-secret")
	c.SetRESTBase(srv.URL)
	c.SetHTTP(srv.Client())
	got, err := c.Transcribe(context.Background(), []byte("OGG"), "", DefaultOptions)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/listen" || gotAuth != "Token listen-secret" {
		t.Fatalf("path=%q auth=%q", gotPath, gotAuth)
	}
	if gotCT != "audio/ogg" {
		t.Fatalf("default content-type = %q", gotCT)
	}
	if got.Text != "via transcribe" {
		t.Fatalf("text = %q", got.Text)
	}
}

func rquery(c *Client) string { return restQuery(DefaultOptions) }

func TestRestQueryFollowsOptions(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"metadata":{"request_id":"r3","duration":1},"results":{"channels":[{"alternatives":[{"transcript":"x"}]}]}}`))
	}))
	t.Cleanup(srv.Close)
	c := New("k")
	c.SetRESTBase(srv.URL)
	c.SetHTTP(srv.Client())
	opts := Options{SmartFormat: false, Paragraphs: false, FillerWords: true, ProfanityFilter: true, Diarize: true}
	if _, err := c.ListenFile(context.Background(), []byte("OGG"), "", opts); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"smart_format=false", "paragraphs=false",
		"filler_words=true", "profanity_filter=true",
		"diarize_model=latest", "detect_language=true", "mip_opt_out=true",
	} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query %q missing %s", gotQuery, want)
		}
	}
}

func TestRestQueryOmitsOffFlags(t *testing.T) {
	q := restQuery(DefaultOptions)
	for _, banned := range []string{"filler_words", "profanity_filter", "diarize"} {
		if strings.Contains(q, banned) {
			t.Fatalf("default query must not send %s: %s", banned, q)
		}
	}
}

// A user who turned every STT toggle off must get a genuinely bare request —
// substituting defaults here would poison the cache row under that variant.
func TestListenFileZeroOptionsStayZero(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"metadata":{"request_id":"r4","duration":1},"results":{"channels":[{"alternatives":[{"transcript":"raw"}]}]}}`))
	}))
	t.Cleanup(srv.Close)
	c := New("k")
	c.SetRESTBase(srv.URL)
	c.SetHTTP(srv.Client())
	if _, err := c.ListenFile(context.Background(), []byte("OGG"), "", Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "smart_format=false") || !strings.Contains(gotQuery, "paragraphs=false") {
		t.Fatalf("zero options must reach the wire, got %q", gotQuery)
	}
}

// Production answered the same video circle with HTTP 408 twice and gave up
// both times, because there was no retry at all.
func TestListenFileRetriesTransientFailures(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.Header().Set("dg-request-id", "req-408")
			w.WriteHeader(http.StatusRequestTimeout)
			_, _ = w.Write([]byte(`{"err_msg":"upstream timed out"}`))
			return
		}
		_, _ = w.Write([]byte(`{"metadata":{"request_id":"r9","duration":1},"results":{"channels":[{"alternatives":[{"transcript":"third time"}]}]}}`))
	}))
	t.Cleanup(srv.Close)
	c := New("k")
	c.SetRESTBase(srv.URL)
	c.SetHTTP(srv.Client())
	got, err := c.ListenFile(context.Background(), []byte("OGG"), "", DefaultOptions)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d", attempts)
	}
	if got.Text != "third time" {
		t.Fatalf("text = %q", got.Text)
	}
}

func TestListenFileDoesNotRetryClientErrors(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("dg-request-id", "req-400")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"err_msg":"unsupported media type"}`))
	}))
	t.Cleanup(srv.Close)
	c := New("k")
	c.SetRESTBase(srv.URL)
	c.SetHTTP(srv.Client())
	_, err := c.ListenFile(context.Background(), []byte("OGG"), "", DefaultOptions)
	if err == nil {
		t.Fatal("a 400 must not be reported as success")
	}
	if attempts != 1 {
		t.Fatalf("a permanent error must not be retried, attempts = %d", attempts)
	}
	// The message has to name the request id and Deepgram's own reason, or a
	// production failure cannot be chased anywhere.
	for _, want := range []string{"400", "req-400", "unsupported media type"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q is missing %q", err, want)
		}
	}
}

func TestRequestTimeoutScalesWithAudio(t *testing.T) {
	if got := requestTimeout(0); got != baseTimeout {
		t.Fatalf("unknown length = %s", got)
	}
	if got := requestTimeout(600); got <= baseTimeout {
		t.Fatalf("ten minutes of audio = %s, want more than the base", got)
	}
	if got := requestTimeout(24 * 3600); got != maxTimeout {
		t.Fatalf("absurd length must clamp, got %s", got)
	}
}

// AudioSeconds sizes the deadline and must never reach the wire: a query change
// would silently split the transcript cache.
func TestAudioSecondsStaysOutOfTheQuery(t *testing.T) {
	opts := DefaultOptions
	opts.AudioSeconds = 4242
	if strings.Contains(restQuery(opts), "4242") {
		t.Fatalf("query leaked the duration hint: %s", restQuery(opts))
	}
}
