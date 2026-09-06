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

func TestSendRichHTMLPreservesReplyAndThread(t *testing.T) {
	var gotPath, gotBody string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":10,"chat":{"id":42,"type":"private"}}}`))
	})
	msg, err := c.SendRichHTML(context.Background(), 42, 7, 3, "<blockquote>1. hello</blockquote>", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/sendRichMessage") {
		t.Fatalf("path = %q", gotPath)
	}
	// The payload is HTML: sending it in the markdown field would let Telegram
	// additionally parse "1." as an ordered list and mangle the transcript.
	for _, want := range []string{`"chat_id":42`, `"html":"\u003cblockquote\u003e1. hello\u003c/blockquote\u003e"`, `"skip_entity_detection":true`, `"message_id":7`, `"message_thread_id":3`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("missing %s in %s", want, gotBody)
		}
	}
	if strings.Contains(gotBody, `"markdown"`) {
		t.Fatalf("rich message must not use the markdown field: %s", gotBody)
	}
	if msg.MessageID != 10 {
		t.Fatalf("message = %+v", msg)
	}
}

// Bot API 10.3 moved receiver_user_id into ephemeral_message_parameters. Sending
// the old flat parameter makes Telegram treat the message as an ordinary one,
// which would publish a /vp placeholder to the whole group.
func TestSendEphemeralMessageUsesEphemeralParameters(t *testing.T) {
	var gotPath, gotBody string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"ephemeral_message_id":8,"chat":{"id":-100,"type":"supergroup"}}}`))
	})
	msg, err := c.SendEphemeralMessage(context.Background(), -100, 9, 77, "only for you", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/sendMessage") {
		t.Fatalf("path = %q", gotPath)
	}
	for _, want := range []string{
		`"ephemeral_message_parameters":{"receiver_user_id":9}`,
		`"ephemeral_message_id":77`,
		`"link_preview_options":{"is_disabled":true}`,
	} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("missing %s in %s", want, gotBody)
		}
	}
	if strings.Contains(gotBody, `"receiver_user_id":9,`) && !strings.Contains(gotBody, `"ephemeral_message_parameters"`) {
		t.Fatalf("flat receiver_user_id is no longer a Bot API parameter: %s", gotBody)
	}
	if strings.Contains(gotBody, "disable_web_page_preview") {
		t.Fatalf("disable_web_page_preview was removed from the Bot API: %s", gotBody)
	}
	if msg.EphemeralMessageID != 8 {
		t.Fatalf("message = %+v", msg)
	}
}

// A transcript longer than 4096 characters can only reach the requester by
// editing the placeholder with rich content: a fresh ephemeral message is
// impossible once Telegram's 15-second reply window has passed.
func TestEditEphemeralRichHTMLSendsRichMessage(t *testing.T) {
	var gotPath, gotBody string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if err := c.EditEphemeralRichHTML(context.Background(), -100, 9, 77, "<blockquote>long</blockquote>", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/editEphemeralMessageText") {
		t.Fatalf("path = %q", gotPath)
	}
	for _, want := range []string{`"receiver_user_id":9`, `"ephemeral_message_id":77`, `"rich_message":{`, `"html":`, `"skip_entity_detection":true`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("missing %s in %s", want, gotBody)
		}
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
	markup := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{{Text: "x", CallbackData: "x"}}}}
	if err := c.EditEphemeralMessageText(context.Background(), -100, 9, 77, "done", markup); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/editEphemeralMessageText") {
		t.Fatalf("path = %q", gotPath)
	}
	for _, want := range []string{`"chat_id":-100`, `"receiver_user_id":9`, `"ephemeral_message_id":77`, `"parse_mode":"HTML"`, `"callback_data":"x"`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("missing %s in %s", want, gotBody)
		}
	}
}

func TestEditMethodsTreatUnchangedMessageAsSuccess(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: message is not modified: specified new message content and reply markup are exactly the same"}`))
	})

	if err := c.EditMessageText(context.Background(), 1, 2, "same", nil); err != nil {
		t.Fatalf("ordinary edit: %v", err)
	}
	if err := c.EditEphemeralMessageText(context.Background(), 1, 3, 4, "same", nil); err != nil {
		t.Fatalf("ephemeral edit: %v", err)
	}
}

func TestDeleteEphemeralMessagePostsIDs(t *testing.T) {
	var gotPath, gotBody string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	if err := c.DeleteEphemeralMessage(context.Background(), -100, 9, 77); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/deleteEphemeralMessage") || !strings.Contains(gotBody, `"receiver_user_id":9`) || !strings.Contains(gotBody, `"ephemeral_message_id":77`) {
		t.Fatalf("path=%q body=%s", gotPath, gotBody)
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
	body, err := c.DownloadFile(context.Background(), f.FilePath, 100)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "OGGDATA" {
		t.Fatalf("got %q", body)
	}
}

func TestDownloadRejectsOversizeBody(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("12345"))
	})
	if _, err := c.DownloadFile(context.Background(), "voice/x.ogg", 4); err == nil || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("error = %v", err)
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
