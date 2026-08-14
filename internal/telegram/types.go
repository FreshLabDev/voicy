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

func (m *Message) Media() (fileID, uniqueID, kind string, duration int, mime string, size int64, ok bool) {
	if m == nil {
		return
	}
	if m.Voice != nil && m.Voice.FileID != "" {
		return m.Voice.FileID, m.Voice.FileUniqueID, "voice", m.Voice.Duration, m.Voice.MimeType, m.Voice.FileSize, true
	}
	if m.VideoNote != nil && m.VideoNote.FileID != "" {
		return m.VideoNote.FileID, m.VideoNote.FileUniqueID, "video_note", m.VideoNote.Duration, "", m.VideoNote.FileSize, true
	}
	return
}
