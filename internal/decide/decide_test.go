// SPDX-License-Identifier: Apache-2.0
package decide

import (
	"encoding/json"
	"testing"

	"github.com/FreshLabDev/tg"
)

func mustUpdate(t *testing.T, raw string) tg.Update {
	t.Helper()
	var upd tg.Update
	if err := json.Unmarshal([]byte(raw), &upd); err != nil {
		t.Fatal(err)
	}
	return upd
}

func TestDecideDMVoiceTranscribes(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 1,
	  "message": {
	    "message_id": 10,
	    "from": {"id": 7, "is_bot": false, "first_name": "A", "language_code": "ru"},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "VOICE1", "file_unique_id": "U1", "duration": 4}
	  }
	}`)
	act := Decide(upd, "voicetextbot")
	if act.Kind != Transcribe || act.Visibility != Public || act.Media == nil || act.Media.FileID != "VOICE1" {
		t.Fatalf("%+v", act)
	}
}

func TestDecideGroupBareVoiceIsSilent(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 2,
	  "message": {
	    "message_id": 11,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "voice": {"file_id": "VOICE1", "file_unique_id": "U1", "duration": 4}
	  }
	}`)
	act := Decide(upd, "voicetextbot")
	if act.Kind != Ignore {
		t.Fatalf("want ignore, got %+v", act)
	}
}

func TestDecideGroupVReplyPublic(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 3,
	  "message": {
	    "message_id": 12,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/v",
	    "reply_to_message": {
	      "message_id": 11,
	      "voice": {"file_id": "VOICE1", "file_unique_id": "U1", "duration": 4}
	    }
	  }
	}`)
	act := Decide(upd, "voicetextbot")
	if act.Kind != Transcribe || act.Visibility != Public || act.Media == nil || act.Media.FileID != "VOICE1" {
		t.Fatalf("%+v", act)
	}
}

func TestDecideGroupVPWithoutEphemeralIsSilent(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 4,
	  "message": {
	    "message_id": 13,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/vp@voicetextbot",
	    "reply_to_message": {
	      "message_id": 11,
	      "video_note": {"file_id": "CIRCLE1", "file_unique_id": "U2", "duration": 2, "length": 240}
	    }
	  }
	}`)
	if Decide(upd, "voicetextbot").Kind != Ignore {
		t.Fatal("public group /vp must stay quiet")
	}
}

func TestDecideGroupVPEphemeralPrivate(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 41,
	  "message": {
	    "message_id": 13,
	    "ephemeral_message_id": 88,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/vp",
	    "reply_to_message": {
	      "message_id": 11,
	      "video_note": {"file_id": "CIRCLE1", "file_unique_id": "U2", "duration": 2, "length": 240}
	    }
	  }
	}`)
	act := Decide(upd, "voicetextbot")
	if act.Kind != Transcribe || act.Visibility != Private || !act.Ephemeral || act.EphemeralMessageID != 88 {
		t.Fatalf("%+v", act)
	}
}

func TestDecideOtherBotCommandIgnored(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 5,
	  "message": {
	    "message_id": 14,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/v@otherbot",
	    "reply_to_message": {"message_id": 11, "voice": {"file_id": "VOICE1", "file_unique_id": "U1"}}
	  }
	}`)
	if Decide(upd, "voicetextbot").Kind != Ignore {
		t.Fatal("expected ignore for @otherbot")
	}
}

func TestDecideGroupStartWithoutEphemeralSilent(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 6,
	  "message": {
	    "message_id": 15,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "group"},
	    "text": "/start"
	  }
	}`)
	if Decide(upd, "voicetextbot").Kind != Ignore {
		t.Fatal("public group /start must stay quiet")
	}
}

func TestDecideEphemeralStart(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 7,
	  "message": {
	    "message_id": 16,
	    "ephemeral_message_id": 99,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/start"
	  }
	}`)
	act := Decide(upd, "voicetextbot")
	if act.Kind != Start || !act.Ephemeral {
		t.Fatalf("%+v", act)
	}
}

func TestDecideDMLanguageOpensPanel(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 8,
	  "message": {
	    "message_id": 17,
	    "from": {"id": 7, "is_bot": false, "first_name": "A", "language_code": "en"},
	    "chat": {"id": 7, "type": "private"},
	    "text": "/language"
	  }
	}`)
	act := Decide(upd, "voicetextbot")
	if act.Kind != Language || act.Arg != "" {
		t.Fatalf("%+v", act)
	}
}

func TestDecideDMLanguageWithArg(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 9,
	  "message": {
	    "message_id": 18,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "text": "/language@voicetextbot ru"
	  }
	}`)
	act := Decide(upd, "voicetextbot")
	if act.Kind != Language || act.Arg != "ru" {
		t.Fatalf("%+v", act)
	}
}

func TestDecideGroupLanguageIsSilent(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 10,
	  "message": {
	    "message_id": 19,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/language"
	  }
	}`)
	if Decide(upd, "voicetextbot").Kind != Ignore {
		t.Fatal("group /language must stay quiet")
	}
}

func TestDecideLanguageOtherBotIgnored(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 11,
	  "message": {
	    "message_id": 20,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "text": "/language@otherbot"
	  }
	}`)
	if Decide(upd, "voicetextbot").Kind != Ignore {
		t.Fatal("expected ignore for @otherbot")
	}
}

func TestDecideMyChatMemberIsMembership(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 90,
	  "my_chat_member": {
	    "chat": {"id": -100, "type": "supergroup", "title": "Team"},
	    "from": {"id": 7, "is_bot": false, "first_name": "A", "language_code": "ru"},
	    "new_chat_member": {"status": "member", "user": {"id": 1, "is_bot": true, "first_name": "Voicy"}}
	  }
	}`)
	act := Decide(upd, "voicybot")
	if act.Kind != Membership {
		t.Fatalf("kind = %s", act.Kind)
	}
	if act.ChatID != -100 || act.UserID != 7 || act.Arg != "member" {
		t.Fatalf("action = %+v", act)
	}
}

// An audio file in a direct chat is speech the user meant to send, so it is
// transcribed without being asked.
func TestDecideDMAudioFileTranscribes(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 100,
	  "message": {
	    "message_id": 30,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "audio": {"file_id": "A1", "file_unique_id": "UA", "duration": 200, "mime_type": "audio/mpeg", "file_name": "talk.mp3", "file_size": 3000000}
	  }
	}`)
	act := Decide(upd, "voicybot")
	if act.Kind != Transcribe || act.Media == nil || act.Media.Kind != "audio" || act.Media.IsVideo {
		t.Fatalf("%+v", act)
	}
	if act.Media.FileName != "talk.mp3" || act.Media.Duration != 200 {
		t.Fatalf("media = %+v", act.Media)
	}
}

// A video or a document is not automatically speech. Answering every file with
// a Deepgram call would be surprising and expensive, so they wait for /v.
func TestDecideDMVideoAndDocumentWaitForCommand(t *testing.T) {
	for _, raw := range []string{
		`{"update_id":101,"message":{"message_id":31,"from":{"id":7,"is_bot":false},"chat":{"id":7,"type":"private"},"video":{"file_id":"V1","file_unique_id":"UV","duration":30,"mime_type":"video/mp4","file_size":900000}}}`,
		`{"update_id":102,"message":{"message_id":32,"from":{"id":7,"is_bot":false},"chat":{"id":7,"type":"private"},"document":{"file_id":"D1","file_unique_id":"UD","mime_type":"audio/x-wav","file_name":"rec.wav","file_size":900000}}}`,
	} {
		if act := Decide(mustUpdate(t, raw), "voicybot"); act.Kind != Ignore {
			t.Fatalf("kind = %s for %s", act.Kind, raw)
		}
	}
}

// The same video answered with /v is transcribed, and marked as video so the
// audio track gets extracted before Deepgram sees it.
func TestDecideVideoWithCommandIsVideoMedia(t *testing.T) {
	upd := mustUpdate(t, `{
	  "update_id": 103,
	  "message": {
	    "message_id": 33,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/v",
	    "reply_to_message": {"message_id": 32, "video": {"file_id": "V2", "file_unique_id": "UV2", "duration": 90, "mime_type": "video/mp4", "file_size": 40000000}}
	  }
	}`)
	act := Decide(upd, "voicybot")
	if act.Kind != Transcribe || act.Media == nil || act.Media.Kind != "video" || !act.Media.IsVideo {
		t.Fatalf("%+v", act)
	}
	if act.Visibility != Public {
		t.Fatalf("visibility = %s", act.Visibility)
	}
}

// A zip is not speech. In a direct chat that is worth saying out loud; in a
// group it stays quiet like every other unhandled message.
func TestDecideRejectsNonMediaDocuments(t *testing.T) {
	dm := mustUpdate(t, `{
	  "update_id": 104,
	  "message": {
	    "message_id": 34,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "text": "/v",
	    "reply_to_message": {"message_id": 33, "document": {"file_id": "Z1", "file_unique_id": "UZ", "mime_type": "application/zip", "file_name": "photos.zip", "file_size": 100}}
	  }
	}`)
	if act := Decide(dm, "voicybot"); act.Kind != Unsupported {
		t.Fatalf("dm kind = %s", act.Kind)
	}

	group := mustUpdate(t, `{
	  "update_id": 105,
	  "message": {
	    "message_id": 35,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/v",
	    "reply_to_message": {"message_id": 34, "document": {"file_id": "Z2", "file_unique_id": "UZ2", "mime_type": "application/zip", "file_name": "photos.zip", "file_size": 100}}
	  }
	}`)
	if act := Decide(group, "voicybot"); act.Kind != Ignore {
		t.Fatalf("group kind = %s", act.Kind)
	}
}
