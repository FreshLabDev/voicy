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
	got, err := c.ListenFile(context.Background(), []byte("OGG"), "audio/ogg")
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
	c.SetDial(func(context.Context, string, http.Header) (Conn, error) {
		t.Fatal("Transcribe must not open a WebSocket")
		return nil, nil
	})
	got, err := c.Transcribe(context.Background(), []byte("OGG"), "", nil)
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

func rquery(c *Client) string { return restQuery() }
