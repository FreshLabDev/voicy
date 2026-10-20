// SPDX-License-Identifier: Apache-2.0
package db

import (
	"testing"

	"github.com/FreshLabDev/voicy/internal/deepgram"
)

// The daily allowance adds up jobs.audio_seconds, so what a job records is what
// the person is charged. A file Deepgram heard twice has to cost twice.
func TestBilledSecondsCountsEveryRunDeepgramBilled(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cacheHit bool
		res      deepgram.Result
		want     float64
	}{
		{"one run", false, deepgram.Result{Duration: 12.5}, 12.5},
		{"one run, billed set", false, deepgram.Result{Duration: 12.5, Billed: 12.5}, 12.5},
		{"named language repeated with detection", false, deepgram.Result{Duration: 12.5, Billed: 25}, 25},
		{"empty result is still billed", false, deepgram.Result{Duration: 3}, 3},
		{"nothing reported", false, deepgram.Result{}, 0},
		{"a cache hit costs nothing", true, deepgram.Result{Duration: 12.5, Billed: 25}, 0},
	} {
		if got := billedSeconds(tc.cacheHit, tc.res); got != tc.want {
			t.Errorf("%s: billed %v, want %v", tc.name, got, tc.want)
		}
	}
}
