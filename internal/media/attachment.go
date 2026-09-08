// SPDX-License-Identifier: Apache-2.0
package media

import "github.com/FreshLabDev/tg"

// Ref is an attachment Voicy might transcribe, flattened out of whichever
// Telegram field carried it.
type Ref struct {
	FileID       string
	FileUniqueID string
	Kind         string
	Duration     int
	MimeType     string
	FileName     string
	FileSize     int64
}

// Of returns the attachment to transcribe. Voice messages and circles come
// first because they are what the bot is for; audio, video and documents
// follow and are gated by the caller, since a document can be any file at all.
//
// The order is a product decision, which is why it lives here and not in the
// Telegram client: another bot would prefer a photo, and one of them would be
// wrong.
func Of(m *tg.Message) (Ref, bool) {
	if m == nil {
		return Ref{}, false
	}
	switch {
	case m.Voice != nil && m.Voice.FileID != "":
		return Ref{m.Voice.FileID, m.Voice.FileUniqueID, KindVoice, m.Voice.Duration, m.Voice.MimeType, "", m.Voice.FileSize}, true
	case m.VideoNote != nil && m.VideoNote.FileID != "":
		return Ref{m.VideoNote.FileID, m.VideoNote.FileUniqueID, KindVideoNote, m.VideoNote.Duration, "video/mp4", "", m.VideoNote.FileSize}, true
	case m.Audio != nil && m.Audio.FileID != "":
		return Ref{m.Audio.FileID, m.Audio.FileUniqueID, KindAudio, m.Audio.Duration, m.Audio.MimeType, m.Audio.FileName, m.Audio.FileSize}, true
	case m.Video != nil && m.Video.FileID != "":
		return Ref{m.Video.FileID, m.Video.FileUniqueID, KindVideo, m.Video.Duration, m.Video.MimeType, m.Video.FileName, m.Video.FileSize}, true
	case m.Document != nil && m.Document.FileID != "":
		return Ref{m.Document.FileID, m.Document.FileUniqueID, KindDocument, 0, m.Document.MimeType, m.Document.FileName, m.Document.FileSize}, true
	}
	return Ref{}, false
}
