// SPDX-License-Identifier: Apache-2.0
package media

import (
	"encoding/json"
	"testing"

	"github.com/FreshLabDev/tg"
)

func TestOfPicksTheAttachment(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want Ref
	}{
		{
			name: "voice",
			raw:  `{"voice":{"file_id":"f1","file_unique_id":"u1","duration":3,"mime_type":"audio/ogg","file_size":99}}`,
			want: Ref{FileID: "f1", FileUniqueID: "u1", Kind: KindVoice, Duration: 3, MimeType: "audio/ogg", FileSize: 99},
		},
		{
			name: "video note",
			raw:  `{"video_note":{"file_id":"f2","file_unique_id":"u2","duration":5,"file_size":50}}`,
			want: Ref{FileID: "f2", FileUniqueID: "u2", Kind: KindVideoNote, Duration: 5, MimeType: "video/mp4", FileSize: 50},
		},
		{
			name: "audio file",
			raw:  `{"audio":{"file_id":"f3","file_unique_id":"u3","duration":180,"mime_type":"audio/mpeg","file_name":"talk.mp3","file_size":4000}}`,
			want: Ref{FileID: "f3", FileUniqueID: "u3", Kind: KindAudio, Duration: 180, MimeType: "audio/mpeg", FileName: "talk.mp3", FileSize: 4000},
		},
		{
			name: "video",
			raw:  `{"video":{"file_id":"f4","file_unique_id":"u4","duration":60,"mime_type":"video/mp4","file_name":"clip.mp4","file_size":900000}}`,
			want: Ref{FileID: "f4", FileUniqueID: "u4", Kind: KindVideo, Duration: 60, MimeType: "video/mp4", FileName: "clip.mp4", FileSize: 900000},
		},
		{
			name: "document",
			raw:  `{"document":{"file_id":"f5","file_unique_id":"u5","mime_type":"audio/x-wav","file_name":"rec.wav","file_size":700}}`,
			want: Ref{FileID: "f5", FileUniqueID: "u5", Kind: KindDocument, MimeType: "audio/x-wav", FileName: "rec.wav", FileSize: 700},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var msg tg.Message
			if err := json.Unmarshal([]byte(tc.raw), &msg); err != nil {
				t.Fatal(err)
			}
			got, ok := Of(&msg)
			if !ok || got != tc.want {
				t.Fatalf("media = %+v (ok=%v), want %+v", got, ok, tc.want)
			}
		})
	}

	// A voice message that also carries a caption-quoted video is still a voice
	// message: speech wins, because that is what the bot is for.
	var both tg.Message
	if err := json.Unmarshal([]byte(`{"voice":{"file_id":"v"},"video":{"file_id":"x"}}`), &both); err != nil {
		t.Fatal(err)
	}
	if got, _ := Of(&both); got.Kind != KindVoice {
		t.Fatalf("kind = %q, want voice to win", got.Kind)
	}

	if _, ok := Of(&tg.Message{}); ok {
		t.Fatal("an empty message must not report media")
	}
	if _, ok := Of(nil); ok {
		t.Fatal("a nil message must not report media")
	}
}
