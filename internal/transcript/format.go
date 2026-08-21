// SPDX-License-Identifier: Apache-2.0
package transcript

import (
	"html"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/FreshLabDev/voicy/internal/deepgram"
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

// RichParts renders one or more complete Rich Markdown messages. Every part is
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
	parts := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		var prefix []string
		if i == 0 && meta != "" {
			prefix = append(prefix, meta)
		}
		if len(chunks) > 1 {
			if lang == "ru" {
				prefix = append(prefix, "<i>Часть "+strconv.Itoa(i+1)+"/"+strconv.Itoa(len(chunks))+"</i>")
			} else {
				prefix = append(prefix, "<i>Part "+strconv.Itoa(i+1)+"/"+strconv.Itoa(len(chunks))+"</i>")
			}
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
		label := "Speaker "
		if lang == "ru" {
			label = "Говорящий "
		}
		sections = append(sections, label+strconv.Itoa(turn.Speaker+1)+":\n"+text)
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
		code = html.EscapeString(code)
		if lang == "ru" {
			parts = append(parts, "Язык: "+code)
		} else {
			parts = append(parts, "Language: "+code)
		}
	}
	if res.Duration > 0 {
		dur := ftoa(res.Duration) + " s"
		if res.Duration >= 60 {
			dur = ftoa(res.Duration/60) + " min"
		}
		if lang == "ru" {
			parts = append(parts, "Длительность: "+dur)
		} else {
			parts = append(parts, "Duration: "+dur)
		}
	}
	if res.Confidence > 0 {
		pct := strconv.Itoa(int(res.Confidence*100 + 0.5))
		if lang == "ru" {
			parts = append(parts, "Точность: "+pct+"%")
		} else {
			parts = append(parts, "Confidence: "+pct+"%")
		}
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

func Header(title, hint string) string {
	s := "<b>" + title + "</b>"
	if hint != "" {
		s += "\n<i>" + hint + "</i>"
	}
	return s
}

func Panel(title, hint, body string) string {
	s := Header(title, hint)
	if body != "" {
		s += "\n\n" + body
	}
	return s
}

func HomeText(lang string) string {
	if lang == "ru" {
		return Panel("Voicy", "Голос в текст", Quote(
			"Пришли голосовое или кружочек — верну текстом.\n"+
				"В группе ответь <code>/v</code> (всем) или <code>/vp</code> (только тебе).",
		))
	}
	return Panel("Voicy", "Voice to text", Quote(
		"Send a voice or a video circle — I’ll send the text back.\n"+
			"In a group, reply with <code>/v</code> (everyone) or <code>/vp</code> (just you).",
	))
}

func HelpText(lang string) string {
	if lang == "ru" {
		return Panel("Справка", "Как пользоваться", Quote(
			"• В личке: просто кинь войс или кружочек.\n"+
				"• В группе: реплай <code>/v</code> — всем, <code>/vp</code> — только тебе.\n"+
				"• Повтор того же файла отдаётся из кэша.",
		))
	}
	return Panel("Help", "How it works", Quote(
		"• In DM: send a voice or a video circle.\n"+
			"• In a group: reply <code>/v</code> for everyone, <code>/vp</code> for you only.\n"+
			"• The same file is served from cache.",
	))
}

func StatsText(lang string, s stats.Snapshot, global bool) string {
	title, hint := "Stats", "Successful transcripts only"
	if lang == "ru" {
		title, hint = "Статистика", "Только успешные расшифровки"
	}
	if global {
		if lang == "ru" {
			title, hint = "Статистика Voicy", "Все пользователи"
		} else {
			title, hint = "Voicy stats", "All users"
		}
	}
	if s.Transcriptions == 0 {
		if lang == "ru" {
			return Panel(title, hint,
				"Пока нет расшифровок. Пришли голосовое или кружочек.")
		}
		return Panel(title, hint,
			"No transcripts yet. Send a voice or a video circle.")
	}
	peak := "—"
	if s.PeakHour >= 0 {
		peak = twoDigits(s.PeakHour) + ":00"
	}
	if lang == "ru" {
		lines := []string{
			"Расшифровок: " + number(s.Transcriptions, " "),
			"Голосовые / кружки: " + number(s.Voice, " ") + " / " + number(s.VideoNotes, " "),
			"Аудио: " + humanDuration(s.DurationSec, "ru"),
			"Слов: " + number(s.Words, " "),
			"Пиковое время: " + peak,
		}
		if global {
			lines = append([]string{"Пользователей: " + number(s.Users, " ")}, lines...)
		} else if s.LastLanguage != "" {
			lines = append(lines, "Последний язык: "+html.EscapeString(s.LastLanguage))
		}
		return Panel(title, hint, Quote(strings.Join(lines, "\n")))
	}
	lines := []string{
		"Transcripts: " + number(s.Transcriptions, ","),
		"Voice / circles: " + number(s.Voice, ",") + " / " + number(s.VideoNotes, ","),
		"Audio: " + humanDuration(s.DurationSec, "en"),
		"Words: " + number(s.Words, ","),
		"Peak time: " + peak,
	}
	if global {
		lines = append([]string{"Users: " + number(s.Users, ",")}, lines...)
	} else if s.LastLanguage != "" {
		lines = append(lines, "Last language: "+html.EscapeString(s.LastLanguage))
	}
	return Panel(title, hint, Quote(strings.Join(lines, "\n")))
}

func EmptySpeechText(lang string) string {
	if lang == "ru" {
		return "Не разобрал речь. Попробуй ещё раз чуть громче или короче."
	}
	return "I didn’t catch any speech. Try again a bit louder or shorter."
}

func StartText(lang string) string { return HomeText(lang) }

func WorkingText(lang string) string {
	if lang == "ru" {
		return "Расшифровываю…"
	}
	return "Transcribing…"
}

func NudgeText(lang string) string {
	if lang == "ru" {
		return "Пришли голосовое или кружочек, либо открой /start."
	}
	return "Send a voice or a video circle, or open /start."
}

func ErrorText(lang string) string {
	if lang == "ru" {
		return "Не получилось расшифровать. Попробуй ещё раз."
	}
	return "Something went wrong while trying to transcribe that. Please try again."
}

func TooLargeText(lang string) string {
	if lang == "ru" {
		return "Это сообщение слишком большое или длинное для обработки. Попробуй более короткую запись."
	}
	return "This message is too large or too long to process. Please send a shorter recording."
}

func PrivateTranscriptLinkText(lang, username, token string) string {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	url := "https://t.me/" + username + "?start=transcript_" + token
	if lang == "ru" {
		return "Расшифровка готова, но не помещается в приватный ответ группы. <a href=\"" + html.EscapeString(url) + "\">Открыть целиком в Voicy</a>."
	}
	return "The transcript is ready but does not fit in a private group reply. <a href=\"" + html.EscapeString(url) + "\">Open it in Voicy</a>."
}

func SentPrivatelyText(lang string) string {
	if lang == "ru" {
		return "Готово. Полная расшифровка отправлена в личный чат с Voicy."
	}
	return "Done. The full transcript was sent to your private chat with Voicy."
}

func NotYoursText(lang string) string {
	if lang == "ru" {
		return "Это не твоё меню."
	}
	return "This isn’t your menu."
}

func LanguageText(lang string) string {
	if lang == "ru" {
		return Panel("Язык", "Интерфейс бота", Quote("Выбери язык интерфейса."))
	}
	return Panel("Language", "Bot interface", Quote("Pick the interface language."))
}

func SettingsText(lang string) string {
	if lang == "ru" {
		return Panel("Настройки", "Форматирование и вывод", Quote(
			"Расшифровка: применяются к новым файлам.\nВывод: применяется сразу.",
		))
	}
	return Panel("Settings", "Formatting and output", Quote(
		"Transcription: applies to new files.\nOutput: applies immediately.",
	))
}

func AboutText(lang string) string {
	if lang == "ru" {
		return Panel("О боте", "Voicy", Quote(
			"Голосовые и кружочки — в текст.\nРаспознавание: Deepgram nova-3.\nЛицензия: Apache-2.0.",
		))
	}
	return Panel("About", "Voicy", Quote(
		"Voice messages and circles — as text.\nRecognition: Deepgram nova-3.\nLicense: Apache-2.0.",
	))
}

// SettingLabel is the display name of one toggle. Unknown keys fall back
// to the raw key so an outdated callback cannot crash rendering.
func SettingLabel(lang, key string) string {
	if lang == "ru" {
		switch key {
		case "smart_format":
			return "Форматирование"
		case "paragraphs":
			return "Абзацы"
		case "filler_words":
			return "Слова-паразиты"
		case "profanity_filter":
			return "Фильтр мата"
		case "diarize":
			return "Говорящие"
		case "quote":
			return "Цитата"
		case "meta":
			return "Метаданные"
		}
		return key
	}
	switch key {
	case "smart_format":
		return "Formatting"
	case "paragraphs":
		return "Paragraphs"
	case "filler_words":
		return "Filler words"
	case "profanity_filter":
		return "Profanity filter"
	case "diarize":
		return "Speakers"
	case "quote":
		return "Quote"
	case "meta":
		return "Metadata"
	}
	return key
}

func BtnStats(lang string) string {
	if lang == "ru" {
		return "Статистика"
	}
	return "Stats"
}

func BtnHelp(lang string) string {
	if lang == "ru" {
		return "Справка"
	}
	return "Help"
}

func BtnClose(lang string) string {
	if lang == "ru" {
		return "Закрыть"
	}
	return "Close"
}

func BtnBack(lang string) string {
	if lang == "ru" {
		return "Назад"
	}
	return "Back"
}

func BtnLanguage(lang string) string {
	if lang == "ru" {
		return "Язык"
	}
	return "Language"
}

func BtnSettings(lang string) string {
	if lang == "ru" {
		return "Настройки"
	}
	return "Settings"
}

func BtnAbout(lang string) string {
	if lang == "ru" {
		return "О боте"
	}
	return "About"
}

func LangOf(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexByte(code, '-'); i > 0 {
		code = code[:i]
	}
	if code == "ru" || code == "uk" || code == "be" {
		return "ru"
	}
	return "en"
}

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
		if lang == "ru" {
			return strconv.FormatInt(minutes, 10) + " мин"
		}
		return strconv.FormatInt(minutes, 10) + " min"
	}
	hours := float64(minutes) / 60
	if lang == "ru" {
		return ftoa(hours) + " ч"
	}
	return ftoa(hours) + " h"
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

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

func ftoa(v float64) string {
	if v < 0 {
		v = 0
	}
	n := int64(v*10 + 0.5)
	s := itoa(n/10) + "." + itoa(n%10)
	return strings.TrimRight(strings.TrimRight(s, "0"), ".")
}
