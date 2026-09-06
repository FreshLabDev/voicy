// SPDX-License-Identifier: Apache-2.0
package stats

import "strings"

// Kind buckets one job's media type into the counters the statistics panel
// shows. Anything that is not a voice message or a video circle is a file.
func Kind(kind string) (voice, videoNote, file int) {
	switch kind {
	case "voice":
		return 1, 0, 0
	case "video_note":
		return 0, 1, 0
	default:
		return 0, 0, 1
	}
}

// ShouldCount reports whether a finished job belongs in user-facing aggregates.
// Empty transcripts and failures are logged only.
func ShouldCount(status, transcript string) bool {
	if status != "sent" {
		return false
	}
	return strings.TrimSpace(transcript) != ""
}

type Snapshot struct {
	Transcriptions int64
	Voice          int64
	VideoNotes     int64
	// Files counts audio, document and video messages, which reach Deepgram
	// through the same path but are not voice notes.
	Files        int64
	DurationSec  float64
	Words        int64
	Users        int64
	LastLanguage string
	PeakHour     int
}

func EmptySnapshot() Snapshot { return Snapshot{PeakHour: -1} }
