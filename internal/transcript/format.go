// SPDX-License-Identifier: Apache-2.0
package transcript

import (
	"fmt"
	"html"
	"strings"

	"github.com/FreshLabDev/voicetotext/internal/deepgram"
)

func Format(res deepgram.Result, lang string) string {
	text := strings.TrimSpace(res.Text)
	if text == "" {
		if lang == "ru" {
			return "Не разобрал речь. Попробуй ещё раз чуть громче или короче."
		}
		return "I didn’t catch any speech. Try again a bit louder or shorter."
	}
	var b strings.Builder
	b.WriteString("<blockquote>")
	b.WriteString(html.EscapeString(text))
	b.WriteString("</blockquote>")
	var meta []string
	if res.Language != "" {
		meta = append(meta, html.EscapeString(res.Language))
	}
	if res.Duration > 0 {
		meta = append(meta, fmt.Sprintf("%.0fs", res.Duration))
	}
	if res.Confidence > 0 {
		meta = append(meta, fmt.Sprintf("%.0f%%", res.Confidence*100))
	}
	if len(meta) > 0 {
		b.WriteString("\n<i>")
		b.WriteString(strings.Join(meta, " · "))
		b.WriteString("</i>")
	}
	return b.String()
}

func StartText(lang string) string {
	if lang == "ru" {
		return "<b>VoiceToText</b>\nПришли голосовое или кружочек — верну текстом.\nВ группе ответь <code>/v</code> (всем) или <code>/vp</code> (только тебе)."
	}
	return "<b>VoiceToText</b>\nSend a voice or a video circle — I’ll send the text back.\nIn a group, reply with <code>/v</code> (everyone) or <code>/vp</code> (just you)."
}

func HelpText(lang string) string {
	if lang == "ru" {
		return "<b>Help</b>\n• В личке: просто кинь войс или кружочек.\n• В группе: реплай <code>/v</code> — всем, <code>/vp</code> — только тебе.\n• Повтор того же файла отдаётся из кэша."
	}
	return "<b>Help</b>\n• In DM: send a voice or a video circle.\n• In a group: reply <code>/v</code> for everyone, <code>/vp</code> for you only.\n• The same file is served from cache."
}

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
