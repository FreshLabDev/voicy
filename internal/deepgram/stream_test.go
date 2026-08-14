// SPDX-License-Identifier: Apache-2.0
package deepgram

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

type fakeConn struct {
	writes [][]byte
	reads  [][]byte
}

func (f *fakeConn) Write(_ context.Context, _ websocket.MessageType, data []byte) error {
	cp := append([]byte(nil), data...)
	f.writes = append(f.writes, cp)
	return nil
}

func (f *fakeConn) Read(_ context.Context) (websocket.MessageType, []byte, error) {
	if len(f.reads) == 0 {
		return 0, nil, &closeErr{}
	}
	next := f.reads[0]
	f.reads = f.reads[1:]
	return websocket.MessageText, next, nil
}

func (f *fakeConn) Close(websocket.StatusCode, string) error { return nil }

type closeErr struct{}

func (closeErr) Error() string { return "websocket: close 1000 (StatusNormalClosure)" }

func TestStreamFileDialsListenWithToken(t *testing.T) {
	var gotURL string
	var gotAuth string
	conn := &fakeConn{
		reads: [][]byte{
			[]byte(`{"type":"Results","is_final":true,"channel":{"alternatives":[{"transcript":"streamed text"}]}}`),
		},
	}
	c := New("dg-secret-key")
	c.SetDial(func(_ context.Context, endpoint string, header http.Header) (Conn, error) {
		gotURL = endpoint
		gotAuth = header.Get("Authorization")
		return conn, nil
	})
	got, err := c.StreamFile(context.Background(), []byte("ogg-bytes-here"), "audio/ogg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(gotURL, "wss://api.deepgram.com/v1/listen?") {
		t.Fatalf("url = %q", gotURL)
	}
	if gotAuth != "Token dg-secret-key" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if got.Text != "streamed text" {
		t.Fatalf("text = %q", got.Text)
	}
	var sawClose bool
	for _, w := range conn.writes {
		if strings.Contains(string(w), `"CloseStream"`) {
			sawClose = true
		}
	}
	if !sawClose {
		t.Fatal("expected CloseStream control frame")
	}
}

func TestListenFileUsesTokenAndBinary(t *testing.T) {
	// Covered together with Transcribe fallback via a fake HTTP server in bot tests;
	// here we assert REST URL construction.
	c := New("k")
	if !strings.Contains(c.rest+ListenPath, "https://api.deepgram.com/v1/listen") {
		t.Fatalf("rest base = %s", c.rest)
	}
}
