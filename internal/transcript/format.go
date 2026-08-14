// SPDX-License-Identifier: Apache-2.0
package transcript

import (
	"html"
	"strconv"
	"strings"

	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/stats"
)

func Format(res deepgram.Result, lang string) string {
	text := normalizeTranscript(res.Text)
	if text == "" {
		return EmptySpeechText(lang)
	}
	return Quote(html.EscapeString(text))
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

func StatsText(lang string, s stats.Snapshot) string {
	if s.Transcriptions == 0 {
		if lang == "ru" {
			return Panel("Статистика", "Только успешные расшифровки",
				"Пока нет расшифровок. Пришли голосовое или кружочек.")
		}
		return Panel("Stats", "Successful transcripts only",
			"No transcripts yet. Send a voice or a video circle.")
	}
	if lang == "ru" {
		return Panel("Статистика", "Только успешные расшифровки", Quote(
			"Расшифровок: "+itoa(s.Transcriptions)+"\n"+
				"Голосовые / кружки: "+itoa(s.Voice)+" / "+itoa(s.VideoNotes)+"\n"+
				"Минут: "+ftoa(s.DurationSec/60),
		))
	}
	return Panel("Stats", "Successful transcripts only", Quote(
		"Transcripts: "+itoa(s.Transcriptions)+"\n"+
			"Voice / circles: "+itoa(s.Voice)+" / "+itoa(s.VideoNotes)+"\n"+
			"Minutes: "+ftoa(s.DurationSec/60),
	))
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

func NotYoursText(lang string) string {
	if lang == "ru" {
		return "Это не твоё меню."
	}
	return "This isn’t your menu."
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
