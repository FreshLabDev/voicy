// SPDX-License-Identifier: Apache-2.0
package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient("SECRETTOKEN")
	c.SetAPIBase(srv.URL)
	c.http = srv.Client()
	return c
}

func TestSendMessageDraftPostsDraftID(t *testing.T) {
	var gotPath, gotBody string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if err := c.SendMessageDraft(context.Background(), 42, 7, "hello"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/sendMessageDraft") {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(gotBody, `"draft_id":7`) || !strings.Contains(gotBody, `"chat_id":42`) {
		t.Fatalf("body = %s", gotBody)
	}
}

func TestSendPrivateMessageSetsReceiver(t *testing.T) {
	var gotBody string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":-100,"type":"supergroup"}}}`))
	})
	if _, err := c.SendPrivateMessage(context.Background(), -100, 9, "secret", 0); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, `"receiver_user_id":9`) {
		t.Fatalf("body = %s", gotBody)
	}
}

func TestEditEphemeralMessageTextPostsIDs(t *testing.T) {
	var gotPath, gotBody string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if err := c.EditEphemeralMessageText(context.Background(), -100, 9, 77, "done"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/editEphemeralMessageText") {
		t.Fatalf("path = %q", gotPath)
	}
	for _, want := range []string{`"chat_id":-100`, `"receiver_user_id":9`, `"ephemeral_message_id":77`, `"parse_mode":"HTML"`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("missing %s in %s", want, gotBody)
		}
	}
}

func TestDeleteMessagePostsIDs(t *testing.T) {
	var gotPath, gotBody string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if err := c.DeleteMessage(context.Background(), -100, 42); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/deleteMessage") {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.Contains(gotBody, `"chat_id":-100`) || !strings.Contains(gotBody, `"message_id":42`) {
		t.Fatalf("body = %s", gotBody)
	}
}

func TestGetFileAndDownload(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/getFile"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"AA","file_path":"voice/x.ogg"}}`))
		case strings.Contains(r.URL.Path, "/file/bot"):
			if strings.Contains(r.URL.Path, "SECRETTOKEN") {
				_, _ = w.Write([]byte("OGGDATA"))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})
	f, err := c.GetFile(context.Background(), "AA")
	if err != nil {
		t.Fatal(err)
	}
	body, err := c.DownloadFile(context.Background(), f.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "OGGDATA" {
		t.Fatalf("got %q", body)
	}
}

func TestTransportErrorRedactsToken(t *testing.T) {
	c := NewClient("SECRETTOKEN")
	err := c.redactError(errorsNew("Get \"https://api.telegram.org/botSECRETTOKEN/getMe\": eof"))
	if strings.Contains(err.Error(), "SECRETTOKEN") {
		t.Fatalf("token leaked: %v", err)
	}
}

func errorsNew(s string) error { return &plainErr{s} }

type plainErr struct{ s string }

func (e *plainErr) Error() string { return e.s }

func TestMessageMediaJSON(t *testing.T) {
	raw := []byte(`{"message_id":1,"chat":{"id":5,"type":"private"},"voice":{"file_id":"f1","file_unique_id":"u1","duration":3,"mime_type":"audio/ogg","file_size":99}}`)
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	id, uid, kind, dur, mime, size, ok := msg.Media()
	if !ok || id != "f1" || uid != "u1" || kind != "voice" || dur != 3 || mime != "audio/ogg" || size != 99 {
		t.Fatalf("media = %s %s %s %d %s %d %v", id, uid, kind, dur, mime, size, ok)
	}
}
