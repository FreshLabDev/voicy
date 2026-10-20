// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"strings"
	"testing"

	"github.com/FreshLabDev/tg"
	"github.com/FreshLabDev/voicy/internal/db"
	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/settings"
)

// dmVoice is a direct-chat voice message from user 7 whose Telegram client
// reports the given language (empty for none).
func dmVoice(t *testing.T, updateID int64, fileID, clientLang string) tg.Update {
	t.Helper()
	lang := ""
	if clientLang != "" {
		lang = `, "language_code": "` + clientLang + `"`
	}
	return parseUpd(t, `{
	  "update_id": `+itoa64(updateID)+`,
	  "message": {
	    "message_id": `+itoa64(updateID)+`,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"`+lang+`},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "`+fileID+`", "file_unique_id": "U", "duration": 10}
	  }
	}`)
}

// Where the language comes from: the setting, then the shared core hub, then the
// language_code of this update. The first source with an opinion decides.
func TestTranscribeNamesTheSpeakersLanguage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		core       string // language core resolved for the person, "" for none
		setting    string
		clientLang string
		want       string
	}{
		{name: "core says Arabic", core: "ar", clientLang: "en", want: "ar"},
		{name: "core says Persian with a region", core: "fa-IR", want: "fa"},
		{name: "no core opinion, Telegram says Persian", clientLang: "fa", want: "fa"},
		{name: "Telegram code with a region", clientLang: "ar-EG", want: "ar"},
		{name: "Belarusian", core: "be", want: "be"},
		{name: "the picked language beats core", setting: "he", core: "ru", clientLang: "ru", want: "he"},
		{name: "the picked language beats Telegram", setting: "kk", clientLang: "en", want: "kk"},
		// A language detection knows is detected, not named.
		{name: "English", core: "en", clientLang: "en", want: ""},
		{name: "Russian", core: "ru", clientLang: "ru", want: ""},
		{name: "nothing known", want: ""},
		// Languages nova-3 lacks are never named.
		{name: "Uzbek", core: "uz", clientLang: "uz", want: ""},
		// Core knows the person and says Russian; an Arabic Telegram client does
		// not outvote it, or every message of theirs would be run twice.
		{name: "core outvotes the client", core: "ru", clientLang: "ar", want: ""},
		// A setting nobody can pick is not obeyed.
		{name: "a junk setting is ignored", setting: "en", core: "ar", want: "ar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &fakeStore{}
			if tc.core != "" {
				st.langs = map[int64]string{7: tc.core}
			}
			if tc.setting != "" {
				cfg := settings.Default()
				cfg.STTLanguage = tc.setting
				st.userCfg = map[int64]settings.Settings{7: cfg}
			}
			stt := &countingSTT{res: deepgram.Result{Text: "hello", Confidence: 0.99, Duration: 10}}
			b := New(st, &fakeTG{}, stt, logger())
			if err := b.Handle(context.Background(), dmVoice(t, 200, "NAMED", tc.clientLang)); err != nil {
				t.Fatal(err)
			}
			if stt.calls != 1 {
				t.Fatalf("calls = %d", stt.calls)
			}
			if stt.opts.Language != tc.want {
				t.Fatalf("named language = %q, want %q", stt.opts.Language, tc.want)
			}
			// The variant describes the request that was sent, to the letter.
			wantVariant := settings.DefaultVariant
			if tc.want != "" {
				wantVariant += "-l" + tc.want
			}
			if len(st.savedVar) != 1 || st.savedVar[0] != wantVariant {
				t.Fatalf("saved variant = %v, want %q", st.savedVar, wantVariant)
			}
		})
	}
}

// A person who gets detection keeps the variant every cached row already has.
func TestDetectionKeepsTheDefaultVariant(t *testing.T) {
	st := &fakeStore{cached: map[string]db.Cached{
		"OLD\x00" + settings.DefaultVariant: {FileID: "OLD", Transcript: "cached before languages", Language: "en"},
	}}
	stt := &countingSTT{res: deepgram.Result{Text: "should-not-run"}}
	b := New(st, &fakeTG{}, stt, logger())
	if err := b.Handle(context.Background(), dmVoice(t, 201, "OLD", "en")); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 0 {
		t.Fatal("a row cached under the default variant must still be a hit")
	}
}

// The same file read as detected French and read as Arabic are different
// transcripts; neither person may be served the other's.
func TestCacheDoesNotMixLanguages(t *testing.T) {
	st := &fakeStore{cached: map[string]db.Cached{
		// What detection made of an Arabic voice message before languages could be
		// named: transliterated, labelled French.
		"SHARED\x00" + settings.DefaultVariant: {FileID: "SHARED", Transcript: "Marhaban kaifa halek", Language: "fr"},
	}}
	stt := &countingSTT{res: deepgram.Result{Text: "مرحبا كيف حالك", Language: "ar", Confidence: 0.95, Duration: 10, Billed: 10}}
	b := New(st, &fakeTG{}, stt, logger())
	st.langs = map[int64]string{7: "ar"}

	if err := b.Handle(context.Background(), dmVoice(t, 202, "SHARED", "ar")); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 1 {
		t.Fatalf("an Arabic speaker was served the detected row, calls = %d", stt.calls)
	}
	want := settings.DefaultVariant + "-lar"
	if got := st.cached["SHARED\x00"+want].Transcript; got != "مرحبا كيف حالك" {
		t.Fatalf("Arabic row = %q", got)
	}
	if got := st.cached["SHARED\x00"+settings.DefaultVariant].Transcript; got != "Marhaban kaifa halek" {
		t.Fatalf("the default row was overwritten: %q", got)
	}

	// Asking again is a hit on the Arabic row, with no Deepgram call.
	if err := b.Handle(context.Background(), dmVoice(t, 203, "SHARED", "ar")); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 1 {
		t.Fatalf("the Arabic row was not reused, calls = %d", stt.calls)
	}

	// An English speaker with the same file is still served the detected row.
	st.langs = map[int64]string{7: "en"}
	if err := b.Handle(context.Background(), dmVoice(t, 204, "SHARED", "en")); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 1 {
		t.Fatalf("the detected row was not reused, calls = %d", stt.calls)
	}
}

// What the job is closed with decides what the daily allowance charges, so the
// seconds of a repeated run must reach CompleteJob.
func TestCompleteJobReceivesEveryBilledRun(t *testing.T) {
	st := &fakeStore{langs: map[int64]string{7: "ar"}}
	stt := &countingSTT{res: deepgram.Result{Text: "hello", Language: "en", Confidence: 1, Duration: 10, Billed: 20}}
	b := New(st, &fakeTG{}, stt, logger())
	if err := b.Handle(context.Background(), dmVoice(t, 205, "TWICE", "ar")); err != nil {
		t.Fatal(err)
	}
	if len(st.completed) != 1 || st.completed[0].Billed != 20 || st.completed[0].Duration != 10 {
		t.Fatalf("job closed with %+v", st.completed)
	}
}

// --- the picker -------------------------------------------------------------

func TestSettingsPanelOffersTheRecognitionLanguage(t *testing.T) {
	text, kb := settingsPanel("en", 7, settings.Default(), scopePrivate)
	if !strings.Contains(text, "Settings") {
		t.Fatalf("text = %q", text)
	}
	var found *tg.InlineKeyboardButton
	for _, btn := range buttonsOf(kb) {
		if btn.CallbackData == "m:7:stt" {
			btn := btn
			found = &btn
		}
	}
	if found == nil {
		t.Fatalf("no recognition row: %#v", kb)
	}
	if found.Text != "Recognition language: Auto" {
		t.Fatalf("row = %q", found.Text)
	}
	if found.Style != "" {
		t.Fatalf("the row opens a list, it is not state: %q", found.Style)
	}

	picked := settings.Default()
	picked.STTLanguage = "fa"
	_, kb = settingsPanel("ru", 7, picked, scopePrivate)
	for _, btn := range buttonsOf(kb) {
		if btn.CallbackData == "m:7:stt" && btn.Text != "Язык распознавания: فارسی" {
			t.Fatalf("row = %q", btn.Text)
		}
	}
}

func TestRecognitionPanelListsEveryHintableLanguage(t *testing.T) {
	_, kb := recognitionPanel("en", 7, settings.Default(), scopePrivate)
	if !markupHas(kb, "m:7:stt:auto") || !markupHas(kb, "m:7:set") {
		t.Fatalf("auto and back are missing: %#v", kb)
	}
	for _, l := range deepgram.HintLanguages {
		if !markupHas(kb, "m:7:stt:"+l.Code) {
			t.Errorf("%s is not offered", l.Code)
		}
	}
	// One green button says where the person stands, and Auto is it by default.
	var green []string
	for _, btn := range buttonsOf(kb) {
		if btn.Style == tg.StyleSuccess {
			green = append(green, btn.CallbackData)
		}
	}
	if len(green) != 1 || green[0] != "m:7:stt:auto" {
		t.Fatalf("styled = %v, want only Auto", green)
	}

	_, kb = recognitionPanel("en", 7, settings.Settings{STTLanguage: "be"}, scopePrivate)
	green = nil
	for _, btn := range buttonsOf(kb) {
		if btn.Style == tg.StyleSuccess {
			green = append(green, btn.CallbackData)
		}
	}
	if len(green) != 1 || green[0] != "m:7:stt:be" {
		t.Fatalf("styled = %v, want only Belarusian", green)
	}
}

func sttCallback(t *testing.T, updateID int64, data string) tg.Update {
	t.Helper()
	return parseUpd(t, `{
	  "update_id": `+itoa64(updateID)+`,
	  "callback_query": {
	    "id": "cb`+itoa64(updateID)+`",
	    "from": {"id": 7, "is_bot": false, "first_name": "A", "language_code": "en"},
	    "message": {"message_id": 99, "chat": {"id": 7, "type": "private"}},
	    "data": "`+data+`"
	  }
	}`)
}

func TestRecognitionCallbacks(t *testing.T) {
	st := &fakeStore{}
	api := &fakeTG{}
	b := New(st, api, &countingSTT{}, logger())
	ctx := context.Background()

	// Opening the list changes nothing.
	if err := b.Handle(ctx, sttCallback(t, 300, "m:7:stt")); err != nil {
		t.Fatal(err)
	}
	if len(api.edits) != 1 || !contains(api.edits[0], "Recognition language") {
		t.Fatalf("picker = %v", api.edits)
	}
	if st.cfg(7).STTLanguage != "" {
		t.Fatal("opening the picker stored a language")
	}

	// Picking stores it and redraws with that language marked.
	if err := b.Handle(ctx, sttCallback(t, 301, "m:7:stt:fa")); err != nil {
		t.Fatal(err)
	}
	if got := st.cfg(7).STTLanguage; got != "fa" {
		t.Fatalf("stored = %q", got)
	}
	last := api.markups[len(api.markups)-1]
	var green string
	for _, btn := range buttonsOf(last) {
		if btn.Style == tg.StyleSuccess {
			green = btn.CallbackData
		}
	}
	if green != "m:7:stt:fa" {
		t.Fatalf("redrawn with %q marked", green)
	}

	// A code nobody can pick is ignored, and so is a language detection knows:
	// naming it would only buy a second run when the audio is not in it.
	edits := len(api.edits)
	for i, data := range []string{"m:7:stt:en", "m:7:stt:ru", "m:7:stt:bogus", "m:7:stt:", "m:7:stt:uz"} {
		if err := b.Handle(ctx, sttCallback(t, int64(310+i), data)); err != nil {
			t.Fatal(err)
		}
	}
	if got := st.cfg(7).STTLanguage; got != "fa" {
		t.Fatalf("a bad code changed the setting to %q", got)
	}
	if len(api.edits) != edits {
		t.Fatalf("a bad code redrew the panel: %v", api.edits[edits:])
	}

	// Auto goes back to deciding from the profile.
	if err := b.Handle(ctx, sttCallback(t, 320, "m:7:stt:auto")); err != nil {
		t.Fatal(err)
	}
	if got := st.cfg(7).STTLanguage; got != "" {
		t.Fatalf("Auto left %q stored", got)
	}
}

// Back from the picker returns to Settings, not to the home screen: that is the
// screen the person came from.
func TestRecognitionBackReturnsToSettings(t *testing.T) {
	_, kb := recognitionPanel("en", 7, settings.Default(), scopePrivate)
	var back []string
	for _, btn := range buttonsOf(kb) {
		if btn.CallbackData == "m:7:set" || btn.CallbackData == "m:7:home" {
			back = append(back, btn.CallbackData)
		}
	}
	if len(back) != 1 || back[0] != "m:7:set" {
		t.Fatalf("back goes to %v", back)
	}
}

// Telegram lets a client send any callback data, so the picker refuses a
// callback that carries somebody else's id, like every other panel does.
func TestRecognitionCallbackIsOwnerScoped(t *testing.T) {
	st := &fakeStore{}
	api := &fakeTG{}
	b := New(st, api, &countingSTT{}, logger())
	upd := parseUpd(t, `{
	  "update_id": 330,
	  "callback_query": {
	    "id": "cb330",
	    "from": {"id": 8, "is_bot": false, "first_name": "B", "language_code": "en"},
	    "message": {"message_id": 99, "chat": {"id": 7, "type": "private"}},
	    "data": "m:7:stt:ar"
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if st.cfg(7).STTLanguage != "" || st.cfg(8).STTLanguage != "" {
		t.Fatal("somebody else's picker changed a setting")
	}
}
