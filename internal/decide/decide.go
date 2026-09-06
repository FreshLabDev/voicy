// SPDX-License-Identifier: Apache-2.0
package decide

import (
	"strings"
	"unicode"

	"github.com/FreshLabDev/voicy/internal/media"
	"github.com/FreshLabDev/voicy/internal/telegram"
)

type Kind string

const (
	Ignore      Kind = "ignore"
	Start       Kind = "start"
	Stats       Kind = "stats"
	Help        Kind = "help"
	About       Kind = "about"
	Retrieve    Kind = "retrieve"
	Transcribe  Kind = "transcribe"
	Nudge       Kind = "nudge"
	Callback    Kind = "callback"
	Language    Kind = "language"
	Membership  Kind = "membership"
	Unsupported Kind = "unsupported"
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
	FileName     string
	FileSize     int64
	// IsVideo marks media whose audio has to be demuxed out of pictures.
	IsVideo bool
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
	Arg                string
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
	// my_chat_member is requested in allowed_updates so the shared core learns
	// where Voicy lives. It never produces a message: groups stay quiet.
	if upd.MyChatMember != nil {
		m := upd.MyChatMember
		return Action{
			Kind:         Membership,
			ChatID:       m.Chat.ID,
			UserID:       m.From.ID,
			User:         m.From,
			Chat:         m.Chat,
			LanguageCode: m.From.LanguageCode,
			Arg:          m.NewChatMember.Status,
			UpdateID:     upd.UpdateID,
		}
	}
	if upd.Callback != nil {
		from := upd.Callback.From
		return Action{
			Kind:               Callback,
			ChatID:             upd.Callback.Message.Chat.ID,
			ThreadID:           upd.Callback.Message.MessageThreadID,
			UserID:             from.ID,
			User:               from,
			Chat:               upd.Callback.Message.Chat,
			LanguageCode:       from.LanguageCode,
			CallbackID:         upd.Callback.ID,
			CallbackData:       upd.Callback.Data,
			CallbackMessageID:  upd.Callback.Message.MessageID,
			Ephemeral:          upd.Callback.Message.EphemeralMessageID != 0,
			EphemeralMessageID: upd.Callback.Message.EphemeralMessageID,
			UpdateID:           upd.UpdateID,
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
		cmd, arg, forUs := parseCommand(text, selfUsername)
		if !forUs {
			return withKind(base, Ignore)
		}
		switch cmd {
		case "start":
			if !private && msg.EphemeralMessageID == 0 {
				// Ordinary group /start: stay quiet; ephemeral handler covers 10.2.
				return withKind(base, Ignore)
			}
			if private {
				if token, ok := strings.CutPrefix(arg, "transcript_"); ok && validToken(token) {
					act := withKind(base, Retrieve)
					act.Arg = token
					return act
				}
			}
			act := withKind(base, Start)
			act.Arg = arg
			return act
		case "stats":
			if !private {
				return withKind(base, Ignore)
			}
			return withKind(base, Stats)
		case "help":
			if !private {
				return withKind(base, Ignore)
			}
			return withKind(base, Help)
		case "about":
			if !private {
				return withKind(base, Ignore)
			}
			return withKind(base, About)
		case "language":
			// Settings live in DM; a group /language stays quiet.
			if !private {
				return withKind(base, Ignore)
			}
			act := withKind(base, Language)
			act.Arg = arg
			return act
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

	if attachment, ok := msg.Media(); ok {
		if !private {
			return withKind(base, Ignore)
		}
		// A document or a video in a direct chat is not automatically speech:
		// answering every file with a Deepgram call would be both surprising and
		// expensive, so those wait for an explicit /v.
		if !media.Implicit(attachment.Kind) {
			return withKind(base, Ignore)
		}
		accepted, isVideo := media.Transcribable(attachment.Kind, attachment.MimeType, attachment.FileName)
		if !accepted {
			return withKind(base, Unsupported)
		}
		act := withKind(base, Transcribe)
		act.Media = mediaOf(attachment, isVideo)
		act.ReplyToID = msg.MessageID
		return act
	}
	if private && text != "" {
		return withKind(base, Nudge)
	}
	return withKind(base, Ignore)
}

func validToken(token string) bool {
	if len(token) < 16 || len(token) > 64 {
		return false
	}
	for _, r := range token {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// transcribeCommand handles an explicit /v or /vp. Unlike a bare attachment it
// accepts every kind, because the user asked for this one by name.
func transcribeCommand(base Action, msg *telegram.Message, private bool, vis Visibility) Action {
	attachment, ok := msg.ReplyToMessage.Media()
	if !ok {
		if private {
			return withKind(base, Nudge)
		}
		return withKind(base, Ignore)
	}
	accepted, isVideo := media.Transcribable(attachment.Kind, attachment.MimeType, attachment.FileName)
	if !accepted {
		if private {
			return withKind(base, Unsupported)
		}
		return withKind(base, Ignore)
	}
	act := withKind(base, Transcribe)
	act.Visibility = vis
	act.Media = mediaOf(attachment, isVideo)
	if msg.ReplyToMessage != nil {
		act.ReplyToID = msg.ReplyToMessage.MessageID
	}
	return act
}

func mediaOf(ref telegram.MediaRef, isVideo bool) *Media {
	return &Media{
		FileID:       ref.FileID,
		FileUniqueID: ref.FileUniqueID,
		Kind:         ref.Kind,
		Duration:     ref.Duration,
		MimeType:     ref.MimeType,
		FileName:     ref.FileName,
		FileSize:     ref.FileSize,
		IsVideo:      isVideo,
	}
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
