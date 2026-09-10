// SPDX-License-Identifier: Apache-2.0
package transcript

import (
	"html"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/i18n"
	"github.com/FreshLabDev/voicy/internal/settings"
	"github.com/FreshLabDev/voicy/internal/stats"
)

const MaxRichCharacters = 32768

func Format(res deepgram.Result, lang string, s settings.Settings) string {
	text := displayText(res, lang)
	if text == "" {
		return EmptySpeechText(lang)
	}
	body := formatBody(text, s.Quote)
	if meta := MetaLine(res, lang, s); meta != "" {
		return meta + "\n" + body
	}
	return body
}

// RichParts renders one or more complete rich messages as HTML. Every part is
// independently valid and stays under Telegram's 32768-character limit.
func RichParts(res deepgram.Result, lang string, s settings.Settings) []string {
	text := displayText(res, lang)
	if text == "" {
		return []string{EmptySpeechText(lang)}
	}
	meta := MetaLine(res, lang, s)
	// Reserve room for HTML wrappers, metadata, and a localized part label.
	budget := MaxRichCharacters - utf8.RuneCountInString(meta) - 128
	if budget < 1024 {
		budget = 1024
	}
	chunks := splitEscaped(text, budget, s.Quote)
	if len(chunks) <= 1 {
		// A transcript that fits one message needs no part label, which is
		// exactly what Format renders.
		return []string{Format(res, lang, s)}
	}
	parts := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		var prefix []string
		if i == 0 && meta != "" {
			prefix = append(prefix, meta)
		}
		if len(chunks) > 1 {
			label := i18n.T(lang, "part.label", "n", strconv.Itoa(i+1), "total", strconv.Itoa(len(chunks)))
			prefix = append(prefix, "<i>"+html.EscapeString(label)+"</i>")
		}
		body := formatBody(chunk, s.Quote)
		if len(prefix) > 0 {
			body = strings.Join(prefix, "\n") + "\n" + body
		}
		parts = append(parts, body)
	}
	return parts
}

func displayText(res deepgram.Result, lang string) string {
	if len(res.Turns) < 2 {
		return normalizeTranscript(res.Text)
	}
	var sections []string
	for _, turn := range res.Turns {
		text := normalizeTranscript(turn.Text)
		if text == "" {
			continue
		}
		label := i18n.T(lang, "speaker.label", "n", strconv.Itoa(turn.Speaker+1))
		sections = append(sections, label+":\n"+text)
	}
	return strings.Join(sections, "\n\n")
}

func formatBody(text string, quote bool) string {
	body := html.EscapeString(text)
	if quote {
		return Quote(body)
	}
	return body
}

func splitEscaped(text string, budget int, quote bool) []string {
	runes := []rune(text)
	var out []string
	for len(runes) > 0 {
		lo, hi, fit := 1, len(runes), 1
		for lo <= hi {
			mid := lo + (hi-lo)/2
			if utf8.RuneCountInString(formatBody(string(runes[:mid]), quote)) <= budget {
				fit = mid
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
		cut := fit
		if fit < len(runes) {
			floor := fit * 3 / 5
			for i := fit - 1; i >= floor; i-- {
				if runes[i] == '\n' || runes[i] == ' ' || runes[i] == '\t' {
					cut = i + 1
					break
				}
			}
		}
		chunk := strings.TrimSpace(string(runes[:cut]))
		if chunk != "" {
			out = append(out, chunk)
		}
		runes = runes[cut:]
	}
	return out
}

// MetaLine renders the optional "language · duration · confidence" line
// above the transcript. Only fields Deepgram actually returned are shown.
func MetaLine(res deepgram.Result, lang string, s settings.Settings) string {
	if !s.Meta {
		return ""
	}
	var parts []string
	if code := strings.TrimSpace(res.Language); code != "" {
		parts = append(parts, i18n.T(lang, "meta.language")+": "+html.EscapeString(code))
	}
	if res.Duration > 0 {
		unit, value := i18n.T(lang, "unit.sec"), ftoa(res.Duration)
		if res.Duration >= 60 {
			unit, value = i18n.T(lang, "unit.min"), ftoa(res.Duration/60)
		}
		parts = append(parts, i18n.T(lang, "meta.duration")+": "+value+" "+unit)
	}
	if res.Confidence > 0 {
		pct := strconv.Itoa(int(res.Confidence*100 + 0.5))
		parts = append(parts, i18n.T(lang, "meta.confidence")+": "+pct+"%")
	}
	if len(parts) == 0 {
		return ""
	}
	return "<i>" + strings.Join(parts, " · ") + "</i>"
}

func Quote(inner string) string {
	if strings.TrimSpace(inner) == "" {
		return ""
	}
	return "<blockquote>" + inner + "</blockquote>"
}

// Panel is the only shape a screen has: a bold title, an italic one-line hint
// under it, then the substance in a quote. Every screen is built here and none
// assembles its own HTML, so a screen cannot drift into a shape of its own —
// which is what "Help is a list, a list is already a shape" turned into.
//
// note is the italic qualifier that sits on the title line. Only About has one,
// the version it is running, and it is what stopped that card from having to
// invent the subtitle "Voicy" under the title "About".
//
// body may be empty: on the language screen the substance is the sixteen
// buttons, and a list in the message repeating them is exactly what the family
// rules forbid.
func Panel(title, note, hint, body string) string {
	s := "<b>" + title + "</b>"
	if note != "" {
		s += " · <i>" + note + "</i>"
	}
	if hint != "" {
		s += "\n<i>" + hint + "</i>"
	}
	if body != "" {
		s += "\n\n" + body
	}
	return s
}

// HomeText introduces the bot to somebody in a direct chat, so it is a card:
// the name, what it does, and how to start.
func HomeText(lang string) string {
	return Panel(i18n.T(lang, "home.title"), "", i18n.T(lang, "home.hint"), Quote(i18n.T(lang, "home.body")))
}

// GroupHomeText is the same door seen from a group, where the only thing Voicy
// does is answer a reply. Settings and language are personal and belong to the
// direct chat, so the group card does not mention them.
func GroupHomeText(lang string) string {
	return Panel(i18n.T(lang, "home.title"), "", i18n.T(lang, "home.hint"), Quote(i18n.T(lang, "home.group")))
}

// HelpText is the list of ways to ask for a transcript. The list used to stand
// bare because it is already a shape of its own; it is quoted like every other
// screen's substance now, so Help is not the one panel built differently.
func HelpText(lang string) string {
	return Panel(i18n.T(lang, "help.title"), "", i18n.T(lang, "help.hint"), Quote(i18n.T(lang, "help.body")))
}

// LanguageText sits over sixteen language buttons that already show which one is
// current. All it has to add is the fact the keyboard cannot show: the choice
// travels to the other bots in the family. That is the hint, and there is no
// body — the buttons are the substance.
func LanguageText(lang string) string {
	return Panel(i18n.T(lang, "lang.title"), "", i18n.T(lang, "lang.hint"), "")
}

// SettingsText sits over the toggle grid. The hint says who the switches belong
// to, and the quote says the one thing a toggle cannot: when flipping it takes
// effect.
func SettingsText(lang string) string {
	return Panel(i18n.T(lang, "settings.title"), "", i18n.T(lang, "settings.hint"), Quote(i18n.T(lang, "settings.body")))
}

// Facts the About card states. They are the same in every language, so they are
// not translation keys: only the labels in front of them are.
const (
	productName     = "Voicy"
	recognitionName = "Deepgram nova-3"
	repoURL         = "https://github.com/FreshLabDev/voicy"
	repoName        = "FreshLabDev/voicy"
	licenseName     = "Apache-2.0"
	adminURL        = "https://t.me/amtiyo"
	adminName       = "@amtiyo"
)

// AboutText is the family-standard credits card: the product and the version it
// is actually running, one line of what that product is, then the facts someone
// might need as "label · value" rows. The repository is a link inside the text
// rather than a button, because a second way to open one address is not a second
// action. version is whatever /healthz reports, so a bug report can name a build.
//
// It is the same Panel as every other screen — the version is the title-line
// note, and the tagline is the hint.
func AboutText(lang, version string) string {
	rows := []string{
		i18n.T(lang, "about.recognition") + " · " + recognitionName,
		i18n.T(lang, "about.source") + " · " + link(repoURL, repoName) + " · " + licenseName,
		i18n.T(lang, "about.admin") + " · " + link(adminURL, adminName),
	}
	return Panel(productName, html.EscapeString(version), i18n.T(lang, "about.tagline"), Quote(strings.Join(rows, "\n")))
}

func link(url, label string) string {
	return "<a href=\"" + url + "\">" + html.EscapeString(label) + "</a>"
}

func StatsText(lang string, s stats.Snapshot, global bool) string {
	titleKey, hintKey := "stats.title.personal", "stats.hint.personal"
	if global {
		titleKey, hintKey = "stats.title.global", "stats.hint.global"
	}
	title, hint := i18n.T(lang, titleKey), i18n.T(lang, hintKey)
	if s.Transcriptions == 0 {
		return Panel(title, "", hint, Quote(i18n.T(lang, "stats.empty")))
	}
	// Thin spaces would be prettier, but a plain separator survives every client.
	sep := ","
	if lang == "ru" || lang == "uk" || lang == "be" {
		sep = " "
	}
	peak := "—"
	if s.PeakHour >= 0 {
		peak = twoDigits(s.PeakHour) + ":00"
	}
	lines := []string{
		i18n.T(lang, "stats.transcripts") + ": " + number(s.Transcriptions, sep),
		i18n.T(lang, "stats.kinds") + ": " + number(s.Voice, sep) + " / " + number(s.VideoNotes, sep) + " / " + number(s.Files, sep),
		i18n.T(lang, "stats.audio") + ": " + humanDuration(s.DurationSec, lang),
		i18n.T(lang, "stats.words") + ": " + number(s.Words, sep),
		i18n.T(lang, "stats.peak") + ": " + peak,
	}
	if global {
		lines = append([]string{i18n.T(lang, "stats.users") + ": " + number(s.Users, sep)}, lines...)
	} else if s.LastLanguage != "" {
		lines = append(lines, i18n.T(lang, "stats.last_language")+": "+html.EscapeString(s.LastLanguage))
	}
	return Panel(title, "", hint, Quote(strings.Join(lines, "\n")))
}

func EmptySpeechText(lang string) string   { return i18n.T(lang, "msg.empty_speech") }
func WorkingText(lang string) string       { return i18n.T(lang, "msg.working") }
func ExtractingText(lang string) string    { return i18n.T(lang, "msg.extracting") }
func NudgeText(lang string) string         { return i18n.T(lang, "msg.nudge") }
func ErrorText(lang string) string         { return i18n.T(lang, "msg.error") }
func TooLargeText(lang string) string      { return i18n.T(lang, "msg.too_large") }
func UnsupportedText(lang string) string   { return i18n.T(lang, "msg.unsupported") }
func SentPrivatelyText(lang string) string { return i18n.T(lang, "msg.sent_privately") }
func NotYoursText(lang string) string      { return i18n.T(lang, "msg.not_yours") }

func PrivateTranscriptLinkText(lang, username, token string) string {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	url := "https://t.me/" + username + "?start=transcript_" + token
	return i18n.T(lang, "msg.private_link", "url", html.EscapeString(url))
}

// SettingLabel is the display name of one toggle. An unknown key falls back to
// the raw key so an outdated callback cannot crash rendering.
func SettingLabel(lang, key string) string {
	if label := i18n.T(lang, "setting."+key); !strings.HasPrefix(label, "[") {
		return label
	}
	return key
}

func BtnStats(lang string) string    { return i18n.T(lang, "btn.stats") }
func BtnHelp(lang string) string     { return i18n.T(lang, "btn.help") }
func BtnClose(lang string) string    { return i18n.T(lang, "btn.close") }
func BtnBack(lang string) string     { return i18n.T(lang, "btn.back") }
func BtnLanguage(lang string) string { return i18n.T(lang, "btn.language") }
func BtnSettings(lang string) string { return i18n.T(lang, "btn.settings") }
func BtnAbout(lang string) string    { return i18n.T(lang, "btn.about") }

// BtnFollowTelegram labels the way back out of a hand-picked language: the
// manual choice outranks every automatic source for ever, so without this
// button a wrong tap is permanent.
func BtnFollowTelegram(lang string) string { return i18n.T(lang, "btn.follow_telegram") }

func TabPersonal(lang string) string { return i18n.T(lang, "stats.tab.personal") }
func TabGlobal(lang string) string   { return i18n.T(lang, "stats.tab.global") }

// LangOf normalizes a Telegram hint or a preference stored by a sibling bot to
// a language Voicy can render.
func LangOf(code string) string { return i18n.Resolve(code) }

func normalizeTranscript(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSpace(text)
	for strings.Contains(text, "\n\n\n") {
		text = strings.ReplaceAll(text, "\n\n\n", "\n\n")
	}
	return text
}

func humanDuration(seconds float64, lang string) string {
	minutes := int64(seconds/60 + 0.5)
	if minutes < 1 {
		minutes = 1
	}
	if minutes < 60 {
		return strconv.FormatInt(minutes, 10) + " " + i18n.T(lang, "unit.min")
	}
	return ftoa(float64(minutes)/60) + " " + i18n.T(lang, "unit.hour")
}

func number(value int64, separator string) string {
	raw := strconv.FormatInt(value, 10)
	for i := len(raw) - 3; i > 0; i -= 3 {
		raw = raw[:i] + separator + raw[i:]
	}
	return raw
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + strconv.Itoa(value)
	}
	return strconv.Itoa(value)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func ftoa(v float64) string {
	if v < 0 {
		v = 0
	}
	n := int64(v*10 + 0.5)
	s := itoa(n/10) + "." + itoa(n%10)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}
