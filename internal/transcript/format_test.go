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

// Every screen is one shape: bold title, italic one-line hint, substance in a
// quote. Help used to stand outside it — a bulleted list is already a shape, the
// argument went — and Settings and Language carried a heading with no hint. The
// shape is not the screen's to choose: a reader who learns it on the front door
// should meet it again everywhere, and a second shape is how About ended up with
// the subtitle "Voicy" under the title "About".
func TestEveryScreenIsOnePanelShape(t *testing.T) {
	busy := stats.Snapshot{Transcriptions: 3, Voice: 2, VideoNotes: 1, DurationSec: 90, Words: 40, PeakHour: 9}
	for _, tc := range []struct {
		name  string
		text  string
		quote bool // false only where the buttons are the substance
	}{
		{"home", HomeText("en"), true},
		{"group home", GroupHomeText("en"), true},
		{"help", HelpText("en"), true},
		{"language", LanguageText("en"), false},
		{"settings", SettingsText("en"), true},
		{"about", AboutText("en", "v0.0.1"), true},
		{"stats empty", StatsText("en", stats.EmptySnapshot(), false), true},
		{"stats personal", StatsText("ru", busy, false), true},
		{"stats global", StatsText("en", busy, true), true},
	} {
		lines := strings.SplitN(tc.text, "\n", 3)
		if !strings.HasPrefix(lines[0], "<b>") {
			t.Errorf("%s has no bold title: %s", tc.name, tc.text)
		}
		if len(lines) < 2 || !strings.HasPrefix(lines[1], "<i>") || !strings.HasSuffix(lines[1], "</i>") {
			t.Errorf("%s has no italic one-line hint: %s", tc.name, tc.text)
			continue
		}
		if !tc.quote {
			if len(lines) != 2 {
				t.Errorf("%s says in the body what its buttons already say: %s", tc.name, tc.text)
			}
			continue
		}
		if len(lines) != 3 || !strings.HasPrefix(lines[2], "\n<blockquote>") || !strings.HasSuffix(tc.text, "</blockquote>") {
			t.Errorf("%s does not carry its substance in a quote: %s", tc.name, tc.text)
		}
	}
	if got := GroupHomeText("en"); !strings.Contains(got, "/vp") {
		t.Errorf("group card must explain the group commands: %s", got)
	}
}
