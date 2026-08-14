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
	if !strings.Contains(rquery(c), "detect_language") {
		t.Fatal("rest query should detect language")
	}
}

func rquery(c *Client) string { return restQuery() }
