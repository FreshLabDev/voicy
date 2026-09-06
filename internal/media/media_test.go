// SPDX-License-Identifier: Apache-2.0
package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A document is whatever a client failed to recognize, so the gate has to be
// explicit. Saying yes to a zip costs a wasted Deepgram call; saying no to a
// .wav makes the bot look broken.
func TestTranscribableGatesDocuments(t *testing.T) {
	for _, tc := range []struct {
		kind, mime, name string
		want, wantVideo  bool
	}{
		{kind: KindVoice, want: true},
		{kind: KindAudio, mime: "audio/mpeg", name: "talk.mp3", want: true},
		{kind: KindVideoNote, want: true, wantVideo: true},
		{kind: KindVideo, mime: "video/mp4", want: true, wantVideo: true},
		{kind: KindDocument, mime: "audio/x-wav", name: "rec.wav", want: true},
		{kind: KindDocument, mime: "video/x-matroska", name: "clip.mkv", want: true, wantVideo: true},
		{kind: KindDocument, mime: "", name: "meeting.m4a", want: true},
		{kind: KindDocument, mime: "", name: "clip.MOV", want: true, wantVideo: true},
		{kind: KindDocument, mime: "application/zip", name: "photos.zip"},
		{kind: KindDocument, mime: "application/pdf", name: "contract.pdf"},
		{kind: KindDocument, mime: "", name: "notes.txt"},
		{kind: KindDocument},
		{kind: "photo", mime: "image/jpeg", name: "a.jpg"},
	} {
		got, gotVideo := Transcribable(tc.kind, tc.mime, tc.name)
		if got != tc.want || gotVideo != tc.wantVideo {
			t.Errorf("Transcribable(%q,%q,%q) = %v,%v want %v,%v",
				tc.kind, tc.mime, tc.name, got, gotVideo, tc.want, tc.wantVideo)
		}
	}
}

// Only what a person sends as speech is transcribed unasked. A document or a
// video waits for /v so Voicy never spends a Deepgram call on a file shared for
// some other reason.
func TestImplicitKinds(t *testing.T) {
	for kind, want := range map[string]bool{
		KindVoice: true, KindVideoNote: true, KindAudio: true,
		KindVideo: false, KindDocument: false, "photo": false,
	} {
		if got := Implicit(kind); got != want {
			t.Errorf("Implicit(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestContentType(t *testing.T) {
	for _, tc := range []struct{ kind, mime, name, want string }{
		{KindVoice, "audio/ogg", "", "audio/ogg"},
		{KindVoice, "", "", "audio/ogg"},
		{KindVideoNote, "", "", "video/mp4"},
		{KindAudio, "AUDIO/MPEG", "x.mp3", "audio/mpeg"},
		{KindDocument, "", "rec.wav", "audio/wav"},
		{KindDocument, "", "clip.webm", "video/webm"},
		{KindDocument, "application/octet-stream", "rec.flac", "audio/flac"},
	} {
		if got := ContentType(tc.kind, tc.mime, tc.name); got != tc.want {
			t.Errorf("ContentType(%q,%q,%q) = %q, want %q", tc.kind, tc.mime, tc.name, got, tc.want)
		}
	}
}

// Video is always worth extracting; audio only once it is large enough that
// re-encoding saves more than it costs. Without ffmpeg nothing is extracted.
func TestExtractionIsNeededForVideoAndLargeAudio(t *testing.T) {
	var absent *Extractor
	if absent.Available() || absent.Needed(true, 1<<30, 1<<20) {
		t.Fatal("a nil extractor must never claim work")
	}
	if NewExtractor("") != nil {
		t.Fatal("an empty binary must disable extraction")
	}
	if NewExtractor("definitely-not-a-real-binary-xyz") != nil {
		t.Fatal("a missing binary must disable extraction")
	}

	e := &Extractor{binary: "ffmpeg"}
	const above = 20 << 20
	for _, tc := range []struct {
		video bool
		size  int64
		want  bool
	}{
		{video: true, size: 1, want: true},
		{video: false, size: above + 1, want: true},
		{video: false, size: above, want: false},
		{video: false, size: 1, want: false},
	} {
		if got := e.Needed(tc.video, tc.size, above); got != tc.want {
			t.Errorf("Needed(%v, %d) = %v, want %v", tc.video, tc.size, got, tc.want)
		}
	}
}

func TestExtractProducesAnAudioTrack(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	e := NewExtractor("ffmpeg")
	if e == nil {
		t.Fatal("ffmpeg is on PATH but the extractor is nil")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "sample.mp4")
	// A one-second silent video with a real audio track.
	gen := exec.Command("ffmpeg", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=5:duration=1",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-shortest", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("could not build a fixture: %v: %s", err, out)
	}

	track, err := e.Extract(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(track) })
	info, err := os.Stat(track)
	if err != nil || info.Size() == 0 {
		t.Fatalf("no audio track produced: %v", err)
	}
	if filepath.Ext(track) != ".ogg" {
		t.Fatalf("track = %q, want an ogg container", track)
	}
	// The point of extracting is that the result is much smaller than the video.
	source, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() >= source.Size() {
		t.Fatalf("extracted %d bytes from a %d byte video", info.Size(), source.Size())
	}
}

func TestExtractRejectsAFileWithNoAudio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	e := NewExtractor("ffmpeg")
	src := filepath.Join(t.TempDir(), "notmedia.bin")
	if err := os.WriteFile(src, []byte("this is not media at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	track, err := e.Extract(context.Background(), src)
	if err == nil {
		_ = os.Remove(track)
		t.Fatal("a file with no audio must not produce a track")
	}
	if _, statErr := os.Stat(strings.TrimSuffix(src, ".bin") + ".extracted.ogg"); statErr == nil {
		t.Fatal("a failed extraction must not leave a file behind")
	}
}
