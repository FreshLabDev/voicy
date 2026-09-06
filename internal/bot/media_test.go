// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/media"
	"github.com/FreshLabDev/voicy/internal/stats"
)

// pathSTT records the file it was handed, so tests can prove what actually
// reached Deepgram rather than what was downloaded.
type pathSTT struct {
	path    string
	ctype   string
	content string
	res     deepgram.Result
}

func (p *pathSTT) Transcribe(_ context.Context, path, ctype string, _ deepgram.Options) (deepgram.Result, error) {
	p.path, p.ctype = path, ctype
	if body, err := os.ReadFile(path); err == nil {
		p.content = string(body)
	}
	if p.res.Text == "" {
		p.res.Text = "transcribed"
	}
	return p.res, nil
}

// Audio never touches memory now: it is streamed to a file and Deepgram reads
// that file. The temporary copy must not survive the job.
func TestAudioIsStreamedToDiskAndCleanedUp(t *testing.T) {
	tmp := t.TempDir()
	tg := &fakeTG{}
	stt := &pathSTT{}
	b := New(&fakeStore{}, tg, stt, logger())
	b.SetMediaTools(nil, tmp, 0)

	upd := parseUpd(t, `{
	  "update_id": 200,
	  "message": {
	    "message_id": 40,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "S1", "file_unique_id": "US", "duration": 8, "mime_type": "audio/ogg"}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if stt.content != "AUDIO" {
		t.Fatalf("Deepgram read %q", stt.content)
	}
	if !strings.HasPrefix(stt.path, tmp) {
		t.Fatalf("audio was staged outside the configured directory: %q", stt.path)
	}
	if stt.ctype != "audio/ogg" {
		t.Fatalf("content type = %q", stt.ctype)
	}
	left, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("temporary files survived the job: %v", left)
	}
}

// A video reaches Deepgram as an extracted audio track, not as pictures.
func TestVideoIsExtractedBeforeDeepgram(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	tmp := t.TempDir()
	tg := &realMediaTG{fixture: buildVideoFixture(t)}
	stt := &pathSTT{}
	b := New(&fakeStore{}, tg, stt, logger())
	b.SetMediaTools(media.NewExtractor("ffmpeg"), tmp, 20<<20)

	upd := parseUpd(t, `{
	  "update_id": 201,
	  "message": {
	    "message_id": 41,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/v",
	    "reply_to_message": {"message_id": 40, "video": {"file_id": "V1", "file_unique_id": "UV", "duration": 1, "mime_type": "video/mp4", "file_name": "clip.mp4"}}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(stt.path, ".extracted.ogg") {
		t.Fatalf("Deepgram was handed %q, not an extracted track", stt.path)
	}
	if stt.ctype != media.ExtractedContentType {
		t.Fatalf("content type = %q", stt.ctype)
	}
	left, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("the video and its track survived the job: %v", left)
	}
}

// realMediaTG serves a fixture file so the extraction path runs end to end.
type realMediaTG struct {
	fakeTG
	fixture string
}

func (f *realMediaTG) DownloadToFile(_ context.Context, _, dst string, _ int64) error {
	body, err := os.ReadFile(f.fixture)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, body, 0o600)
}

func buildVideoFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=5:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-shortest", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("could not build a fixture: %v: %s", err, out)
	}
	return path
}

// A file that carries no speech at all gets a plain answer rather than a
// Deepgram call.
func TestUnsupportedDocumentIsAnsweredNotTranscribed(t *testing.T) {
	tg := &fakeTG{}
	stt := &countingSTT{}
	b := New(&fakeStore{}, tg, stt, logger())
	b.self = "voicetextbot"
	upd := parseUpd(t, `{
	  "update_id": 202,
	  "message": {
	    "message_id": 42,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "text": "/v",
	    "reply_to_message": {"message_id": 41, "document": {"file_id": "Z", "file_unique_id": "UZ", "mime_type": "application/zip", "file_name": "photos.zip"}}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 0 || tg.downloaded != 0 {
		t.Fatalf("a zip must not be downloaded or transcribed: calls=%d downloads=%d", stt.calls, tg.downloaded)
	}
	if len(tg.sent) != 1 || tg.sent[0] == "" {
		t.Fatalf("the user must be told why nothing happened: %v", tg.sent)
	}
}

// Files are their own statistics category: they are neither voice notes nor
// circles, and lumping them in would misreport both.
func TestStatsCountFilesSeparately(t *testing.T) {
	for kind, want := range map[string][3]int{
		"voice":      {1, 0, 0},
		"video_note": {0, 1, 0},
		"audio":      {0, 0, 1},
		"video":      {0, 0, 1},
		"document":   {0, 0, 1},
		"":           {0, 0, 1},
	} {
		v, c, f := stats.Kind(kind)
		if [3]int{v, c, f} != want {
			t.Errorf("Kind(%q) = %d,%d,%d want %v", kind, v, c, f, want)
		}
	}
}
