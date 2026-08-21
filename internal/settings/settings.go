// SPDX-License-Identifier: Apache-2.0
package settings

import (
	"fmt"
	"strings"
)

// Settings is one user's transcription and delivery preference set.
// Bool keys map 1:1 to user_settings columns.
type Settings struct {
	SmartFormat bool
	Paragraphs  bool
	FillerWords bool
	Profanity   bool
	Diarize     bool
	Quote       bool
	Meta        bool
}

// Spec describes one toggle. STT marks options that change the Deepgram
// request and therefore the transcript cache variant.
type Spec struct {
	Key     string
	Default bool
	STT     bool
}

var Specs = []Spec{
	{Key: "smart_format", Default: true, STT: true},
	{Key: "paragraphs", Default: true, STT: true},
	{Key: "filler_words", Default: false, STT: true},
	{Key: "profanity_filter", Default: false, STT: true},
	{Key: "diarize", Default: false, STT: true},
	{Key: "quote", Default: true, STT: false},
	{Key: "meta", Default: false, STT: false},
}

// DefaultVariant is the cache variant of Default(). It must match the
// transcripts.variant DEFAULT in migrations/002_user_settings.sql so rows
// written before variants stay valid hits for default users.
const DefaultVariant = "sf1-p1-fw0-pf0-d0"

func Default() Settings {
	return Settings{
		SmartFormat: true,
		Paragraphs:  true,
		Quote:       true,
	}
}

// SpecOf returns the spec for a toggle key. Unknown keys are rejected here,
// before any SQL sees them.
func SpecOf(key string) (Spec, bool) {
	for _, spec := range Specs {
		if spec.Key == key {
			return spec, true
		}
	}
	return Spec{}, false
}

// Keys lists every valid toggle key in UI order.
func Keys() []string {
	out := make([]string, 0, len(Specs))
	for _, spec := range Specs {
		out = append(out, spec.Key)
	}
	return out
}

// Apply returns a copy with one key set, validating the key.
func (s Settings) Apply(key string, value bool) (Settings, error) {
	if _, ok := SpecOf(key); !ok {
		return s, fmt.Errorf("unknown setting %q", key)
	}
	switch key {
	case "smart_format":
		s.SmartFormat = value
	case "paragraphs":
		s.Paragraphs = value
	case "filler_words":
		s.FillerWords = value
	case "profanity_filter":
		s.Profanity = value
	case "diarize":
		s.Diarize = value
	case "quote":
		s.Quote = value
	case "meta":
		s.Meta = value
	}
	return s, nil
}

// IsOn reports the value of one key. Unknown keys report false.
func (s Settings) IsOn(key string) bool {
	switch key {
	case "smart_format":
		return s.SmartFormat
	case "paragraphs":
		return s.Paragraphs
	case "filler_words":
		return s.FillerWords
	case "profanity_filter":
		return s.Profanity
	case "diarize":
		return s.Diarize
	case "quote":
		return s.Quote
	case "meta":
		return s.Meta
	}
	return false
}

// Variant identifies the Deepgram option set of a Settings. Only STT-affecting
// options take part: delivery options reuse the same transcript.
func (s Settings) Variant() string {
	var b strings.Builder
	write := func(prefix string, on bool) {
		if on {
			b.WriteString(prefix + "1")
		} else {
			b.WriteString(prefix + "0")
		}
	}
	write("sf", s.SmartFormat)
	write("-p", s.Paragraphs)
	write("-fw", s.FillerWords)
	write("-pf", s.Profanity)
	write("-d", s.Diarize)
	return b.String()
}
