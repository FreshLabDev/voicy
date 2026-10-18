// SPDX-License-Identifier: Apache-2.0
package deepgram

import "strings"

// HintLanguage is a language nova-3 transcribes but Deepgram's automatic
// language detection does not recognise. Such audio only comes out right when
// the request names the language; detect_language=true instead answers with a
// wrong guess (Arabic comes back as "fr", Hebrew as "pl", Kazakh as "tr") and
// a transliterated transcript.
type HintLanguage struct {
	Code string
	// Name is the language's own name, shown in the recognition-language picker.
	Name string
}

// HintLanguages is the picker order. It is nova-3's language list minus the 35
// languages detect_language supports, as published on
//
//	https://developers.deepgram.com/docs/models-languages-overview
//	https://developers.deepgram.com/docs/language-detection
//
// and checked against both pages on 2026-10-18. `multi` is a code-switching mode
// rather than a language and is left out. Deepgram's regional variants (ar-EG,
// ka-GE, …) are not used: the bare code is what the picker and Telegram speak.
var HintLanguages = []HintLanguage{
	{"ar", "العربية"},
	{"fa", "فارسی"},
	{"he", "עברית"},
	{"ur", "اردو"},
	{"be", "Беларуская"},
	{"kk", "Қазақша"},
	{"ka", "ქართული"},
	{"hy", "Հայերեն"},
	{"mn", "Монгол"},
	{"ps", "پښتو"},
	{"bn", "বাংলা"},
	{"ta", "தமிழ்"},
	{"te", "తెలుగు"},
	{"mr", "मराठी"},
	{"gu", "ગુજરાતી"},
	{"kn", "ಕನ್ನಡ"},
	{"pa", "ਪੰਜਾਬੀ"},
	{"ne", "नेपाली"},
	{"as", "অসমীয়া"},
	{"af", "Afrikaans"},
	{"tl", "Tagalog"},
	{"hr", "Hrvatski"},
	{"sr", "Српски"},
	{"bs", "Bosanski"},
	{"sl", "Slovenščina"},
	{"mk", "Македонски"},
}

var hintCodes = func() map[string]bool {
	m := make(map[string]bool, len(HintLanguages))
	for _, l := range HintLanguages {
		m[l.Code] = true
	}
	return m
}()

// hintAliases maps the older or longer codes clients still send to the code
// Deepgram knows. Telegram clients have reported Hebrew as "iw" and Filipino as
// "fil".
var hintAliases = map[string]string{"iw": "he", "fil": "tl"}

// HintFor returns the language to name in the request for a Telegram language
// code or a stored preference ("fa-IR", "ar_EG", "iw"), or "" when automatic
// detection handles it or Deepgram does not know it at all.
func HintFor(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexAny(code, "-_"); i > 0 {
		code = code[:i]
	}
	if alias, ok := hintAliases[code]; ok {
		code = alias
	}
	if hintCodes[code] {
		return code
	}
	return ""
}

// NameOf is the language's own name for a hint code, or the code itself.
func NameOf(code string) string {
	for _, l := range HintLanguages {
		if l.Code == code {
			return l.Name
		}
	}
	return code
}
