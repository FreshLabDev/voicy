// SPDX-License-Identifier: Apache-2.0
package telegram

import "time"

type User struct {
	ID           int64  `json:"id"`
	IsBot        bool   `json:"is_bot"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Username     string `json:"username"`
	LanguageCode string `json:"language_code"`
}

type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Username string `json:"username"`
}

type Voice struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
}

type VideoNote struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Length       int    `json:"length"`
	Duration     int    `json:"duration"`
	FileSize     int64  `json:"file_size"`
}

type Audio struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
	FileName     string `json:"file_name"`
}

type Video struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
	FileName     string `json:"file_name"`
}

type Document struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	MimeType     string `json:"mime_type"`
	FileSize     int64  `json:"file_size"`
	FileName     string `json:"file_name"`
}

type File struct {
	FileID   string `json:"file_id"`
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}

type Message struct {
	MessageID          int64      `json:"message_id"`
	EphemeralMessageID int64      `json:"ephemeral_message_id"`
	MessageThreadID    int        `json:"message_thread_id"`
	From               *User      `json:"from"`
	ReceiverUser       *User      `json:"receiver_user"`
	Chat               Chat       `json:"chat"`
	Text               string     `json:"text"`
	Caption            string     `json:"caption"`
	Voice              *Voice     `json:"voice"`
	VideoNote          *VideoNote `json:"video_note"`
	Audio              *Audio     `json:"audio"`
	Video              *Video     `json:"video"`
	Document           *Document  `json:"document"`
	ReplyToMessage     *Message   `json:"reply_to_message"`
}

type CallbackQuery struct {
	ID      string  `json:"id"`
	From    User    `json:"from"`
	Message Message `json:"message"`
	Data    string  `json:"data"`
}

type ChatMember struct {
	Status string `json:"status"`
	User   User   `json:"user"`
}

type ChatMemberUpdated struct {
	Chat          Chat       `json:"chat"`
	From          User       `json:"from"`
	NewChatMember ChatMember `json:"new_chat_member"`
}

type Update struct {
	UpdateID     int64              `json:"update_id"`
	Message      *Message           `json:"message"`
	Callback     *CallbackQuery     `json:"callback_query"`
	MyChatMember *ChatMemberUpdated `json:"my_chat_member"`
}

type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
	Style        string `json:"style,omitempty"`
}

const (
	StylePrimary = "primary"
	StyleSuccess = "success"
	StyleDanger  = "danger"
)

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
	IsEphemeral bool   `json:"is_ephemeral,omitempty"`
}

type BotCommandScope struct {
	Type string `json:"type"`
	// LanguageCode is carried here for convenience; setMyCommands takes it as a
	// sibling of scope, and the client sends it separately.
	LanguageCode string `json:"-"`
}

type Me struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type APIError struct {
	Method          string
	StatusCode      int
	ErrorCode       int
	Description     string
	RetryAfter      time.Duration
	MigrateToChatID int64
}

func (e *APIError) Error() string {
	if e.RetryAfter > 0 {
		return "telegram " + e.Method + " failed: " + e.Description + " (retry after " + e.RetryAfter.String() + ")"
	}
	return "telegram " + e.Method + " failed: " + e.Description
}

// MediaRef is any attachment Voicy might transcribe.
type MediaRef struct {
	FileID       string
	FileUniqueID string
	Kind         string
	Duration     int
	MimeType     string
	FileName     string
	FileSize     int64
}

// Media returns the attachment to transcribe. Voice messages and circles come
// first because they are what the bot is for; audio, video and documents follow
// and are gated by the caller, since a document can be any file at all.
func (m *Message) Media() (MediaRef, bool) {
	if m == nil {
		return MediaRef{}, false
	}
	switch {
	case m.Voice != nil && m.Voice.FileID != "":
		return MediaRef{m.Voice.FileID, m.Voice.FileUniqueID, "voice", m.Voice.Duration, m.Voice.MimeType, "", m.Voice.FileSize}, true
	case m.VideoNote != nil && m.VideoNote.FileID != "":
		return MediaRef{m.VideoNote.FileID, m.VideoNote.FileUniqueID, "video_note", m.VideoNote.Duration, "video/mp4", "", m.VideoNote.FileSize}, true
	case m.Audio != nil && m.Audio.FileID != "":
		return MediaRef{m.Audio.FileID, m.Audio.FileUniqueID, "audio", m.Audio.Duration, m.Audio.MimeType, m.Audio.FileName, m.Audio.FileSize}, true
	case m.Video != nil && m.Video.FileID != "":
		return MediaRef{m.Video.FileID, m.Video.FileUniqueID, "video", m.Video.Duration, m.Video.MimeType, m.Video.FileName, m.Video.FileSize}, true
	case m.Document != nil && m.Document.FileID != "":
		return MediaRef{m.Document.FileID, m.Document.FileUniqueID, "document", 0, m.Document.MimeType, m.Document.FileName, m.Document.FileSize}, true
	}
	return MediaRef{}, false
}
