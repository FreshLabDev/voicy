// SPDX-License-Identifier: Apache-2.0
package decide

import (
	"encoding/json"
	"testing"

	"github.com/FreshLabDev/voicetotext/internal/telegram"
)

func mustUpdate(t *testing.T, raw string) telegram.Update {
	t.Helper()
	var upd telegram.Update
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

func TestDecideGroupVPReplyPrivate(t *testing.T) {
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
	act := Decide(upd, "voicetextbot")
	if act.Kind != Transcribe || act.Visibility != Private || act.Media == nil || act.Media.Kind != "video_note" {
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
