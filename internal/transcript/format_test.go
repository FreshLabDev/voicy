// SPDX-License-Identifier: Apache-2.0
package transcript

import (
	"strings"
	"testing"

	"github.com/FreshLabDev/voicy/internal/deepgram"
)

func TestFormatEscapesAndMeta(t *testing.T) {
	got := Format(deepgram.Result{Text: "<hi> & you", Language: "en", Duration: 12, Confidence: 0.91}, "en")
	if !strings.Contains(got, "&lt;hi&gt; &amp; you") {
		t.Fatalf("not escaped: %s", got)
	}
	if !strings.Contains(got, "<blockquote>") || !strings.Contains(got, "en") || !strings.Contains(got, "91%") {
		t.Fatalf("format = %s", got)
	}
}

func TestFormatEmpty(t *testing.T) {
	got := Format(deepgram.Result{}, "en")
	if !strings.Contains(got, "didn’t catch") && !strings.Contains(got, "didn't catch") {
		t.Fatalf("empty = %s", got)
	}
}
