// SPDX-License-Identifier: Apache-2.0
package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// download is the pre-streaming helper the assertions still read best with.
func download(t *testing.T, c *Client, filePath string, maxBytes int64) ([]byte, error) {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "out.bin")
	if err := c.DownloadToFile(context.Background(), filePath, dst, maxBytes); err != nil {
		return nil, err
	}
	return os.ReadFile(dst)
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
	body, err := download(t, c, f.FilePath, 100)
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
	_, err := download(t, c, "voice/x.ogg", 4)
	if err == nil || !errors.Is(err, errFileTooLarge) {
		t.Fatalf("error = %v, want the size limit", err)
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
	for _, tc := range []struct {
		name string
		raw  string
		want MediaRef
	}{
		{
			name: "voice",
			raw:  `{"voice":{"file_id":"f1","file_unique_id":"u1","duration":3,"mime_type":"audio/ogg","file_size":99}}`,
			want: MediaRef{FileID: "f1", FileUniqueID: "u1", Kind: "voice", Duration: 3, MimeType: "audio/ogg", FileSize: 99},
		},
		{
			name: "video note",
			raw:  `{"video_note":{"file_id":"f2","file_unique_id":"u2","duration":5,"file_size":50}}`,
			want: MediaRef{FileID: "f2", FileUniqueID: "u2", Kind: "video_note", Duration: 5, MimeType: "video/mp4", FileSize: 50},
		},
		{
			name: "audio file",
			raw:  `{"audio":{"file_id":"f3","file_unique_id":"u3","duration":180,"mime_type":"audio/mpeg","file_name":"talk.mp3","file_size":4000}}`,
			want: MediaRef{FileID: "f3", FileUniqueID: "u3", Kind: "audio", Duration: 180, MimeType: "audio/mpeg", FileName: "talk.mp3", FileSize: 4000},
		},
		{
			name: "video",
			raw:  `{"video":{"file_id":"f4","file_unique_id":"u4","duration":60,"mime_type":"video/mp4","file_name":"clip.mp4","file_size":900000}}`,
			want: MediaRef{FileID: "f4", FileUniqueID: "u4", Kind: "video", Duration: 60, MimeType: "video/mp4", FileName: "clip.mp4", FileSize: 900000},
		},
		{
			name: "document",
			raw:  `{"document":{"file_id":"f5","file_unique_id":"u5","mime_type":"audio/x-wav","file_name":"rec.wav","file_size":700}}`,
			want: MediaRef{FileID: "f5", FileUniqueID: "u5", Kind: "document", MimeType: "audio/x-wav", FileName: "rec.wav", FileSize: 700},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var msg Message
			if err := json.Unmarshal([]byte(tc.raw), &msg); err != nil {
				t.Fatal(err)
			}
			got, ok := msg.Media()
			if !ok || got != tc.want {
				t.Fatalf("media = %+v (ok=%v), want %+v", got, ok, tc.want)
			}
		})
	}

	// A message with nothing attached has no media, and neither does a nil one.
	var empty Message
	if _, ok := empty.Media(); ok {
		t.Fatal("an empty message must not report media")
	}
	var nilMsg *Message
	if _, ok := nilMsg.Media(); ok {
		t.Fatal("a nil message must not report media")
	}
}

// A local Bot API server started with TELEGRAM_LOCAL answers getFile with an
// absolute path on its own filesystem. Reading it from the shared volume is
// what lifts the cloud API's 20 MB ceiling.
func TestDownloadReadsLocalBotAPIPath(t *testing.T) {
	var httpCalls int
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		httpCalls++
		_, _ = w.Write([]byte("SHOULD-NOT-BE-USED"))
	})
	dir := t.TempDir()
	path := filepath.Join(dir, "voice.oga")
	if err := os.WriteFile(path, []byte("LOCALBYTES"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := download(t, c, path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "LOCALBYTES" {
		t.Fatalf("body = %q", got)
	}
	if httpCalls != 0 {
		t.Fatal("an absolute path must be read from disk, not fetched over HTTP")
	}
	// The local server never cleans these up and the volume is shared.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the file must be removed after reading, stat err = %v", err)
	}
}

func TestDownloadRejectsOversizeLocalFile(t *testing.T) {
	var httpCalls int
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		httpCalls++
		_, _ = w.Write(make([]byte, 64))
	})
	path := filepath.Join(t.TempDir(), "big.oga")
	if err := os.WriteFile(path, make([]byte, 64), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := download(t, c, path, 16); err == nil {
		t.Fatal("an oversize local file must be rejected")
	}
	if httpCalls != 0 {
		t.Fatal("a file already known to be oversize must not be fetched again over HTTP")
	}
	// A rejected file stays on disk: it was never consumed.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat = %v", err)
	}
}

// The server's data directory holds one subdirectory per bot, named after that
// bot's token, so Voicy does not mount it. An absolute path that cannot be read
// is made relative again and fetched from the same server over HTTP.
func TestDownloadFallsBackToHTTPWhenTheVolumeIsNotMounted(t *testing.T) {
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("OVER-HTTP"))
	})
	got, err := download(t, c, "/var/lib/telegram-bot-api/SECRETTOKEN/voice/file_7.oga", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "OVER-HTTP" {
		t.Fatalf("body = %q", got)
	}
	if !strings.HasSuffix(gotPath, "/voice/file_7.oga") {
		t.Fatalf("path = %q; the server root and token prefix must be stripped", gotPath)
	}
	if strings.Contains(strings.TrimSuffix(gotPath, "/voice/file_7.oga"), "var/lib") {
		t.Fatalf("the absolute prefix leaked into the request: %q", gotPath)
	}
}

func TestRelativeLocalPath(t *testing.T) {
	for _, tc := range []struct{ path, token, want string }{
		{"/var/lib/telegram-bot-api/123:ABC/voice/f.oga", "123:ABC", "voice/f.oga"},
		{"/srv/tg/123:ABC/video_notes/f.mp4", "123:ABC", "video_notes/f.mp4"},
		{"/somewhere/else/f.oga", "123:ABC", "/somewhere/else/f.oga"},
		{"/var/lib/telegram-bot-api/123:ABC/voice/f.oga", "", "/var/lib/telegram-bot-api/123:ABC/voice/f.oga"},
	} {
		if got := relativeLocalPath(tc.path, tc.token); got != tc.want {
			t.Fatalf("relativeLocalPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestDownloadStillUsesHTTPForRelativePaths(t *testing.T) {
	var gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte("REMOTE"))
	})
	got, err := download(t, c, "voice/file_1.oga", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "REMOTE" {
		t.Fatalf("body = %q", got)
	}
	if !strings.HasSuffix(gotPath, "/voice/file_1.oga") {
		t.Fatalf("path = %q", gotPath)
	}
}
