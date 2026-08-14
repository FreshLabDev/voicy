// SPDX-License-Identifier: Apache-2.0
package stats

import "strings"

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
	DurationSec    float64
	Words          int64
	Users          int64
	LastLanguage   string
	PeakHour       int
}

func EmptySnapshot() Snapshot { return Snapshot{PeakHour: -1} }
