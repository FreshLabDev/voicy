// SPDX-License-Identifier: Apache-2.0
package i18n

import (
	"strings"
	"testing"
)

// A missing string shows as "[key]" to whoever is reading the panel, so every
// key must exist in every language Voicy offers to pick.
func TestEveryKeyCoversEveryLanguage(t *testing.T) {
	for _, key := range Keys() {
		for _, code := range Codes() {
			if strings.TrimSpace(translations[key][code]) == "" {
				t.Errorf("%s is missing %s", key, code)
			}
		}
	}
}

// A placeholder that survives into one language but not another renders a
// literal "{url}" to that user.
func TestPlaceholdersMatchAcrossLanguages(t *testing.T) {
	for _, key := range Keys() {
		want := placeholders(translations[key][DefaultLang])
		for _, code := range Codes() {
			if got := placeholders(translations[key][code]); got != want {
				t.Errorf("%s [%s] has placeholders %q, English has %q", key, code, got, want)
			}
		}
	}
}

func placeholders(text string) string {
	var found []string
	for {
		open := strings.IndexByte(text, '{')
		if open < 0 {
			break
		}
		close := strings.IndexByte(text[open:], '}')
		if close < 0 {
			break
		}
		found = append(found, text[open:open+close+1])
		text = text[open+close+1:]
	}
	// Order does not matter across languages, only the set does.
	for i := 0; i < len(found); i++ {
		for j := i + 1; j < len(found); j++ {
			if found[j] < found[i] {
				found[i], found[j] = found[j], found[i]
			}
		}
	}
	return strings.Join(found, ",")
}

func TestResolveFallsBackToEnglish(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ru", "ru"},
		{"ru-RU", "ru"},
		{"uk", "uk"},
		{"be", "be"},
		{"zh_CN", "zh"},
		{"EN-gb", "en"},
		{"", "en"},
		{"kl", "en"},
		{"klingon", "en"},
	} {
		if got := Resolve(tc.in); got != tc.want {
			t.Errorf("Resolve(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Ukrainian and Belarusian used to collapse into Russian. They are their own
// languages here, and a preference written to the shared core must say so.
func TestUkrainianAndBelarusianAreNotRussian(t *testing.T) {
	for _, code := range []string{"uk", "be"} {
		if Resolve(code) != code {
			t.Fatalf("%s resolved to %q", code, Resolve(code))
		}
		if T(code, "home.hint") == T("ru", "home.hint") {
			t.Fatalf("%s reuses the Russian text", code)
		}
	}
}

func TestInterpolation(t *testing.T) {
	got := T("en", "part.label", "n", "2", "total", "5")
	if got != "Part 2/5" {
		t.Fatalf("got %q", got)
	}
	if got := T("en", "no.such.key"); got != "[no.such.key]" {
		t.Fatalf("unknown key = %q", got)
	}
}

// Every option in the picker must be renderable, and every renderable language
// must be offered.
func TestPickerMatchesSupportedSet(t *testing.T) {
	for _, o := range LANGUAGE_OPTIONS {
		if !IsSupported(o.Code) {
			t.Errorf("%s is offered but not supported", o.Code)
		}
		if o.Label == "" || o.Label == o.Code {
			t.Errorf("%s has no native label", o.Code)
		}
	}
	if len(LANGUAGE_OPTIONS) != len(Codes()) {
		t.Fatal("the picker and the supported set disagree")
	}
}
