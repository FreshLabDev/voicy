// SPDX-License-Identifier: Apache-2.0

// Package media decides what Voicy will transcribe and prepares it for
// Deepgram. Telegram hands over five kinds of attachment; a voice message and
// an mp3 can go to Deepgram untouched, while a video or a large recording is
// worth reducing to a small mono audio track first. Deepgram recommends
// exactly that for large video, and it is also what keeps a 400 MB upload from
// crossing the network twice.
package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Kinds of Telegram attachment Voicy understands.
const (
	KindVoice     = "voice"
	KindVideoNote = "video_note"
	KindAudio     = "audio"
	KindVideo     = "video"
	KindDocument  = "document"
)

// ImplicitKinds are transcribed in a direct chat without being asked. They are
// the ones a person sends *as speech*; a document or a video is answered only
// on an explicit /v, so Voicy never spends a Deepgram call on a file that was
// shared for some other reason.
func Implicit(kind string) bool {
	return kind == KindVoice || kind == KindVideoNote || kind == KindAudio
}

// audioExtensions and videoExtensions gate documents, which Telegram uses for
// anything a client did not recognize — including a photo album, a PDF or a zip.
var audioExtensions = map[string]bool{
	".mp3": true, ".m4a": true, ".aac": true, ".wav": true, ".flac": true,
	".ogg": true, ".oga": true, ".opus": true, ".wma": true, ".amr": true,
	".aiff": true, ".aif": true, ".caf": true, ".mka": true, ".weba": true,
}

var videoExtensions = map[string]bool{
	".mp4": true, ".m4v": true, ".mov": true, ".mkv": true, ".webm": true,
	".avi": true, ".wmv": true, ".flv": true, ".mpg": true, ".mpeg": true,
	".3gp": true, ".ts": true, ".ogv": true,
}

// Transcribable reports whether an attachment plausibly carries speech, and
// whether it is video. A wrong yes costs one Deepgram call that returns
// nothing; a wrong no makes Voicy look broken, so an unknown document with an
// audio-looking name is accepted.
func Transcribable(kind, mimeType, fileName string) (ok bool, isVideo bool) {
	switch kind {
	case KindVoice, KindAudio:
		return true, false
	case KindVideoNote, KindVideo:
		return true, true
	case KindDocument:
		mime := strings.ToLower(strings.TrimSpace(mimeType))
		switch {
		case strings.HasPrefix(mime, "audio/"):
			return true, false
		case strings.HasPrefix(mime, "video/"):
			return true, true
		}
		ext := strings.ToLower(filepath.Ext(fileName))
		if audioExtensions[ext] {
			return true, false
		}
		if videoExtensions[ext] {
			return true, true
		}
		return false, false
	}
	return false, false
}

// ContentType is what Deepgram is told the bytes are. Telegram's own mime type
// is used when it looks sane, otherwise the kind decides.
func ContentType(kind, mimeType, fileName string) string {
	if mime := strings.ToLower(strings.TrimSpace(mimeType)); strings.HasPrefix(mime, "audio/") || strings.HasPrefix(mime, "video/") {
		return mime
	}
	switch kind {
	case KindVideoNote, KindVideo:
		return "video/mp4"
	case KindVoice:
		return "audio/ogg"
	}
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".mp3":
		return "audio/mpeg"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	case ".flac":
		return "audio/flac"
	case ".mp4", ".m4v", ".mov":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	}
	return "audio/ogg"
}

// ExtractedContentType is what ffmpeg produces.
const ExtractedContentType = "audio/ogg"

// Extractor turns an arbitrary media file into a small mono Opus track.
type Extractor struct {
	binary string
}

// NewExtractor returns an extractor, or nil when no ffmpeg binary is
// configured or present. A nil extractor is usable: Needed reports false and
// files are sent to Deepgram untouched.
func NewExtractor(binary string) *Extractor {
	if binary == "" {
		return nil
	}
	if _, err := exec.LookPath(binary); err != nil {
		return nil
	}
	return &Extractor{binary: binary}
}

func (e *Extractor) Available() bool { return e != nil && e.binary != "" }

// Needed reports whether extracting is worth an ffmpeg run. Video always is:
// the sound track is a rounding error next to the pictures. Audio only is once
// the file is big enough that re-encoding saves more than it costs.
func (e *Extractor) Needed(isVideo bool, size, extractAbove int64) bool {
	if !e.Available() {
		return false
	}
	return isVideo || (extractAbove > 0 && size > extractAbove)
}

// Extract writes a 16 kHz mono Opus track next to the source and returns its
// path. Speech recognition gains nothing from stereo or a high sample rate, so
// this is both smaller and no less accurate.
func (e *Extractor) Extract(ctx context.Context, src string) (string, error) {
	if !e.Available() {
		return "", fmt.Errorf("no ffmpeg available")
	}
	dst := strings.TrimSuffix(src, filepath.Ext(src)) + ".extracted.ogg"
	cmd := exec.CommandContext(ctx, e.binary,
		"-nostdin", "-loglevel", "error", "-y",
		"-i", src,
		"-vn", "-map", "0:a:0",
		"-ac", "1", "-ar", "16000",
		"-c:a", "libopus", "-b:a", "24k",
		"-f", "ogg", dst,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		_ = os.Remove(dst)
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 200 {
			detail = detail[:200]
		}
		if detail == "" {
			return "", fmt.Errorf("ffmpeg failed: %w", err)
		}
		return "", fmt.Errorf("ffmpeg failed: %s", strings.Join(strings.Fields(detail), " "))
	}
	info, err := os.Stat(dst)
	if err != nil || info.Size() == 0 {
		_ = os.Remove(dst)
		return "", fmt.Errorf("ffmpeg produced no audio track")
	}
	return dst, nil
}
