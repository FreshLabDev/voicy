// SPDX-License-Identifier: Apache-2.0
package decide

import (
	"strings"
	"unicode"

	"github.com/FreshLabDev/voicy/internal/telegram"
)

type Kind string

const (
	Ignore             Kind = "ignore"
	Start              Kind = "start"
	Stats              Kind = "stats"
	Help               Kind = "help"
	Transcribe         Kind = "transcribe"
	Nudge              Kind = "nudge"
	Callback           Kind = "callback"
	Language           Kind = "language"
)

type Visibility string

const (
	Public  Visibility = "public"
	Private Visibility = "private"
)

type Media struct {
	FileID       string
	FileUniqueID string
	Kind         string
	Duration     int
	MimeType     string
	FileSize     int64
}

type Action struct {
	Kind               Kind
	Visibility         Visibility
	Media              *Media
	ChatID             int64
	ThreadID           int
	UserID             int64
	User               telegram.User
	Chat               telegram.Chat
	LanguageCode       string
	ReplyToID          int64
	Ephemeral          bool
	EphemeralMessageID int64
	CallbackID         string
	CallbackData       string
	CallbackMessageID  int64
	UpdateID           int64
	MessageID          int64
}

func Decide(upd telegram.Update, selfUsername string) Action {
	if upd.Callback != nil {
		from := upd.Callback.From
		return Action{
			Kind:              Callback,
			ChatID:            upd.Callback.Message.Chat.ID,
			UserID:            from.ID,
			User:              from,
			Chat:              upd.Callback.Message.Chat,
			LanguageCode:      from.LanguageCode,
			CallbackID:        upd.Callback.ID,
			CallbackData:      upd.Callback.Data,
			CallbackMessageID: upd.Callback.Message.MessageID,
			UpdateID:          upd.UpdateID,
		}
	}
	msg := upd.Message
	if msg == nil || msg.From == nil || msg.From.IsBot {
		return Action{Kind: Ignore, UpdateID: upd.UpdateID}
	}
	base := Action{
		ChatID:             msg.Chat.ID,
		ThreadID:           msg.MessageThreadID,
		UserID:             msg.From.ID,
		User:               *msg.From,
		Chat:               msg.Chat,
		LanguageCode:       msg.From.LanguageCode,
		Ephemeral:          msg.EphemeralMessageID != 0,
		EphemeralMessageID: msg.EphemeralMessageID,
		UpdateID:           upd.UpdateID,
		MessageID:          msg.MessageID,
		Visibility:         Public,
	}
	private := msg.Chat.Type == "private"
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		text = strings.TrimSpace(msg.Caption)
	}

	if text != "" && strings.HasPrefix(text, "/") {
		cmd, _, forUs := parseCommand(text, selfUsername)
		if !forUs {
			return withKind(base, Ignore)
		}
		switch cmd {
		case "start":
			if !private && msg.EphemeralMessageID == 0 {
				// Ordinary group /start: stay quiet; ephemeral handler covers 10.2.
				return withKind(base, Ignore)
			}
			return withKind(base, Start)
		case "stats":
			return withKind(base, Stats)
		case "help", "about":
			return withKind(base, Help)
		case "v":
			return transcribeCommand(base, msg, private, Public)
		case "vp":
			// Group /vp is registered ephemeral. A non-ephemeral public /vp
			// must stay quiet — receiver_user_id alone is not enough for a
			// non-admin bot (Bot API 10.2).
			if !private && msg.EphemeralMessageID == 0 {
				return withKind(base, Ignore)
			}
			return transcribeCommand(base, msg, private, Private)
		default:
			if private {
				return withKind(base, Nudge)
			}
			return withKind(base, Ignore)
		}
	}

	if media := mediaFrom(msg); media != nil {
		if !private {
			return withKind(base, Ignore)
		}
		act := withKind(base, Transcribe)
		act.Media = media
		act.ReplyToID = msg.MessageID
		return act
	}
	if private && text != "" {
		return withKind(base, Nudge)
	}
	return withKind(base, Ignore)
}

func transcribeCommand(base Action, msg *telegram.Message, private bool, vis Visibility) Action {
	media := mediaFrom(msg.ReplyToMessage)
	if media == nil {
		if private {
			return withKind(base, Nudge)
		}
		return withKind(base, Ignore)
	}
	act := withKind(base, Transcribe)
	act.Visibility = vis
	act.Media = media
	if msg.ReplyToMessage != nil {
		act.ReplyToID = msg.ReplyToMessage.MessageID
	}
	return act
}

func mediaFrom(m *telegram.Message) *Media {
	if m == nil {
		return nil
	}
	id, uid, kind, dur, mime, size, ok := m.Media()
	if !ok {
		return nil
	}
	return &Media{FileID: id, FileUniqueID: uid, Kind: kind, Duration: dur, MimeType: mime, FileSize: size}
}

func withKind(a Action, k Kind) Action {
	a.Kind = k
	return a
}

func parseCommand(text, self string) (cmd, arg string, forUs bool) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", "", false
	}
	token := fields[0]
	if !strings.HasPrefix(token, "/") {
		return "", "", false
	}
	token = strings.TrimLeft(token, "/")
	name, at, hasAt := strings.Cut(token, "@")
	name = strings.ToLower(stripNonLetter(name))
	if hasAt {
		forUs = self != "" && strings.EqualFold(at, self)
	} else {
		forUs = true
	}
	if len(fields) > 1 {
		arg = strings.TrimSpace(strings.Join(fields[1:], " "))
	}
	return name, arg, forUs
}

func stripNonLetter(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}
		return -1
	}, s)
}
