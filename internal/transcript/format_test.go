// SPDX-License-Identifier: Apache-2.0
package transcript

import (
	"strings"
	"testing"

	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/settings"
	"github.com/FreshLabDev/voicy/internal/stats"
)

func TestFormatEscapesWithoutMeta(t *testing.T) {
	got := Format(deepgram.Result{Text: "<hi> & you", Language: "en", Duration: 12, Confidence: 0.91}, "en", settings.Default())
	if !strings.Contains(got, "&lt;hi&gt; &amp; you") {
		t.Fatalf("not escaped: %s", got)
	}
	if !strings.Contains(got, "<blockquote>") {
		t.Fatalf("format = %s", got)
	}
	if strings.Contains(got, "91%") || strings.Contains(got, "12 s") {
		t.Fatalf("meta leaked: %s", got)
	}
}

func TestFormatKeepsParagraphs(t *testing.T) {
	got := Format(deepgram.Result{Text: "first paragraph\n\nsecond paragraph"}, "en", settings.Default())
	if !strings.Contains(got, "first paragraph\n\nsecond paragraph") {
		t.Fatalf("paragraphs lost: %s", got)
	}
}

func TestFormatEmpty(t *testing.T) {
	got := Format(deepgram.Result{}, "en", settings.Default())
	if !strings.Contains(got, "didn’t catch") && !strings.Contains(got, "didn't catch") {
		t.Fatalf("empty = %s", got)
	}
}

func TestFormatMetaLine(t *testing.T) {
	s := settings.Default()
	s.Meta = true
	got := Format(deepgram.Result{Text: "hello", Language: "ru", Duration: 90, Confidence: 0.93}, "en", s)
	for _, want := range []string{"Language: ru", "Duration: 1.5 min", "Confidence: 93%"} {
		if !strings.Contains(got, want) {
			t.Fatalf("meta %q missing: %s", want, got)
		}
	}
	if !strings.Contains(got, "<blockquote>") {
		t.Fatalf("quote lost: %s", got)
	}
}

func TestFormatQuoteOff(t *testing.T) {
	s := settings.Default()
	s.Quote = false
	got := Format(deepgram.Result{Text: "hello"}, "en", s)
	if strings.Contains(got, "<blockquote>") {
		t.Fatalf("quote must be off: %s", got)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("text lost: %s", got)
	}
}

func TestStatsEmptyHasNoZeros(t *testing.T) {
	got := StatsText("en", stats.EmptySnapshot(), false)
	if strings.Contains(got, "Transcripts: 0") || strings.Contains(got, "0 / 0") {
		t.Fatalf("empty stats should not show zeros: %s", got)
	}
	if !strings.Contains(got, "No transcripts yet") {
		t.Fatalf("empty stats = %s", got)
	}
}

// The About card is the one screen that had to invent a subtitle to fit the
// shared panel shape: it read "About" with "Voicy" underneath. It now leads with
// the product and the build it is actually running.
func TestAboutCardShape(t *testing.T) {
	got := AboutText("en", "v0.0.1-beta.5")
	if !strings.HasPrefix(got, "<b>Voicy</b> · <i>v0.0.1-beta.5</i>\n") {
		t.Fatalf("about head = %s", got)
	}
	for _, want := range []string{
		`<a href="https://github.com/FreshLabDev/voicy">FreshLabDev/voicy</a>`,
		`<a href="https://t.me/amtiyo">@amtiyo</a>`,
		"Apache-2.0",
		"Deepgram nova-3",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("about card missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "<i>Voicy</i>") {
		t.Errorf("the subtitle that only repeated the title is back: %s", got)
	}
}

// Each screen carries the shape its content asks for: a list is not boxed in a
// quote, and a one-sentence prompt over a keyboard needs no subtitle.
func TestPanelShapesFollowTheirContent(t *testing.T) {
	if got := HelpText("en"); strings.Contains(got, "<blockquote>") || strings.Contains(got, "<i>") {
		t.Errorf("help is a list, not a card: %s", got)
	}
	if got := LanguageText("en"); strings.Contains(got, "<blockquote>") || strings.Contains(got, "<i>") {
		t.Errorf("language prompt = %s", got)
	}
	if got := SettingsText("en"); strings.Contains(got, "<i>") {
		t.Errorf("settings needs no subtitle: %s", got)
	}
	if got := GroupHomeText("en"); !strings.Contains(got, "/vp") {
		t.Errorf("group card must explain the group commands: %s", got)
	}
}
