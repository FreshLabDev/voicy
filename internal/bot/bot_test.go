// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/FreshLabDev/voicy/internal/db"
	"github.com/FreshLabDev/voicy/internal/decide"
	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/settings"
	"github.com/FreshLabDev/voicy/internal/stats"
	"github.com/FreshLabDev/voicy/internal/telegram"
	"github.com/FreshLabDev/voicy/internal/transcript"
)

type fakeStore struct {
	mu        sync.Mutex
	cached    map[string]db.Cached
	saved     []string
	savedVar  []string
	jobs      map[int64]string
	delivered map[int64]bool
	successes int
	touched   int
	offset    int64
	userCfg   map[int64]settings.Settings
	langs     map[int64]string
	toggled   []string
}

func (f *fakeStore) cfg(userID int64) settings.Settings {
	if s, ok := f.userCfg[userID]; ok {
		return s
	}
	return settings.Default()
}

func (f *fakeStore) Touch(context.Context, telegram.User, *telegram.Chat) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touched++
	return nil
}
func (f *fakeStore) GetCached(_ context.Context, id, variant string) (db.Cached, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cached == nil {
		return db.Cached{}, false, nil
	}
	c, ok := f.cached[id+"\x00"+variant]
	return c, ok, nil
}
func (f *fakeStore) TranscriptByToken(context.Context, int64, string) (db.Cached, bool, error) {
	return db.Cached{}, false, nil
}
func (f *fakeStore) SaveTranscript(_ context.Context, fileID, _, _, variant string, res deepgram.Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, fileID)
	f.savedVar = append(f.savedVar, variant)
	if f.cached == nil {
		f.cached = map[string]db.Cached{}
	}
	f.cached[fileID+"\x00"+variant] = db.Cached{
		FileID: fileID, Transcript: res.Text, Language: res.Language,
		Confidence: res.Confidence, Duration: res.Duration, WordCount: res.WordCount,
	}
	return nil
}
func (f *fakeStore) CreateJob(_ context.Context, updateID, _, _, _ int64, _, _, _, token string) (db.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.jobs == nil {
		f.jobs = map[int64]string{}
	}
	if st, ok := f.jobs[updateID]; ok {
		return db.Job{ID: updateID, Status: st, RetrievalToken: token, Delivered: f.delivered[updateID]}, nil
	}
	f.jobs[updateID] = "received"
	return db.Job{ID: updateID, Status: "received", RetrievalToken: token}, nil
}

func (f *fakeStore) MarkDelivered(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.delivered == nil {
		f.delivered = map[int64]bool{}
	}
	f.delivered[id] = true
	return nil
}
func (f *fakeStore) CompleteJob(_ context.Context, id int64, status, _ string, _ bool, res deepgram.Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.jobs == nil {
		f.jobs = map[int64]string{}
	}
	f.jobs[id] = status
	if status == "sent" && strings.TrimSpace(res.Text) != "" {
		f.successes++
	}
	return nil
}
func (f *fakeStore) FailUpdate(context.Context, int64, string) error { return nil }
func (f *fakeStore) UserSettings(_ context.Context, userID int64) (settings.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cfg(userID), nil
}
func (f *fakeStore) SetSetting(_ context.Context, userID int64, key string, value bool) (settings.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.cfg(userID)
	if _, ok := settings.SpecOf(key); !ok {
		return s, fmt.Errorf("unknown setting %q", key)
	}
	next, err := s.Apply(key, value)
	if err != nil {
		return s, err
	}
	if f.userCfg == nil {
		f.userCfg = map[int64]settings.Settings{}
	}
	f.userCfg[userID] = next
	f.toggled = append(f.toggled, key)
	return next, nil
}
func (f *fakeStore) SetLanguage(_ context.Context, userID int64, lang string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.langs == nil {
		f.langs = map[int64]string{}
	}
	f.langs[userID] = lang
	return nil
}
func (f *fakeStore) EffectiveLanguage(_ context.Context, userID int64) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	lang, ok := f.langs[userID]
	return lang, ok && lang != "", nil
}
func (f *fakeStore) UserStats(context.Context, int64) (stats.Snapshot, error) {
	return stats.EmptySnapshot(), nil
}
func (f *fakeStore) GlobalStats(context.Context) (stats.Snapshot, error) {
	return stats.EmptySnapshot(), nil
}
func (f *fakeStore) Offset(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.offset, nil
}
func (f *fakeStore) AdvanceOffset(_ context.Context, off int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offset = off
	return nil
}

type fakeTG struct {
	mu             sync.Mutex
	sent           []string
	ephemeral      []string
	ephemeralEdits []string
	ephemeralRich  []string
	edits          []string
	deleted        []string
	answered       []string
	richChats      []int64
	markups        []*telegram.InlineKeyboardMarkup
	groupCommands  []telegram.BotCommand
	downloaded     int
	gotFile        int
	calls          []string
	richErr        error
	richEdits      []string
	chatActions    int
	onRich         func()
}

func (f *fakeTG) DeleteWebhook(context.Context) error                               { return nil }
func (f *fakeTG) GetUpdates(context.Context, int64, int) ([]telegram.Update, error) { return nil, nil }
func (f *fakeTG) GetMe(context.Context) (telegram.Me, error) {
	return telegram.Me{Username: "voicetextbot"}, nil
}
func (f *fakeTG) SetMyCommandsForScope(_ context.Context, commands []telegram.BotCommand, scope *telegram.BotCommandScope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if scope != nil && scope.Type == "all_group_chats" {
		f.groupCommands = append([]telegram.BotCommand{}, commands...)
	}
	return nil
}
func (f *fakeTG) SendMessage(_ context.Context, _ int64, text string, markup *telegram.InlineKeyboardMarkup) (telegram.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "sendMessage")
	f.sent = append(f.sent, text)
	f.markups = append(f.markups, markup)
	return telegram.Message{}, nil
}
func (f *fakeTG) SendEphemeralMessage(_ context.Context, _, _, _ int64, text string, _ *telegram.InlineKeyboardMarkup) (telegram.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "sendEphemeral")
	f.ephemeral = append(f.ephemeral, text)
	return telegram.Message{MessageID: 501, EphemeralMessageID: 501}, nil
}
func (f *fakeTG) SendRichHTML(_ context.Context, chatID, _ int64, _ int, text string, _ *telegram.InlineKeyboardMarkup) (telegram.Message, error) {
	f.mu.Lock()
	hook := f.onRich
	f.calls = append(f.calls, "sendRich")
	if f.richErr != nil {
		err := f.richErr
		f.richErr = nil
		f.mu.Unlock()
		return telegram.Message{}, err
	}
	f.sent = append(f.sent, text)
	f.richChats = append(f.richChats, chatID)
	id := int64(len(f.sent))
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return telegram.Message{MessageID: id}, nil
}

// transcriptParts is what the user actually received, in order. A direct chat
// now shows "Transcribing…" first and the result replaces it, so part one
// arrives as an edit and any further parts as new messages.
func (f *fakeTG) transcriptParts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string{}, f.richEdits...)
	for _, text := range f.sent {
		if text == transcript.WorkingText("en") || text == transcript.WorkingText("ru") {
			continue
		}
		out = append(out, text)
	}
	return out
}

func (f *fakeTG) EditMessageRichHTML(_ context.Context, _, _ int64, text string, _ *telegram.InlineKeyboardMarkup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "editRich")
	f.richEdits = append(f.richEdits, text)
	return nil
}
func (f *fakeTG) EditEphemeralRichHTML(_ context.Context, _, _, _ int64, text string, _ *telegram.InlineKeyboardMarkup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "editEphemeralRich")
	f.ephemeralRich = append(f.ephemeralRich, text)
	return nil
}
func (f *fakeTG) EditEphemeralMessageText(_ context.Context, _, _, _ int64, text string, _ *telegram.InlineKeyboardMarkup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "editEphemeral")
	f.ephemeralEdits = append(f.ephemeralEdits, text)
	return nil
}
func (f *fakeTG) EditMessageText(_ context.Context, _, _ int64, text string, markup *telegram.InlineKeyboardMarkup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "editMessage")
	f.edits = append(f.edits, text)
	f.markups = append(f.markups, markup)
	return nil
}
func (f *fakeTG) DeleteMessage(_ context.Context, chatID, messageID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "deleteMessage")
	f.deleted = append(f.deleted, itoa64(chatID)+":"+itoa64(messageID))
	return nil
}
func (f *fakeTG) DeleteEphemeralMessage(_ context.Context, chatID, _ int64, messageID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "deleteEphemeral")
	f.deleted = append(f.deleted, itoa64(chatID)+":"+itoa64(messageID))
	return nil
}
func (f *fakeTG) AnswerCallbackQuery(_ context.Context, _, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "answerCallback")
	f.answered = append(f.answered, text)
	return nil
}
func (f *fakeTG) SendChatAction(context.Context, int64, int, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chatActions++
	return nil
}
func (f *fakeTG) GetFile(context.Context, string) (telegram.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotFile++
	return telegram.File{FilePath: "voice/x.ogg"}, nil
}
func (f *fakeTG) DownloadFile(context.Context, string, int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloaded++
	return []byte("AUDIO"), nil
}

type countingSTT struct {
	mu             sync.Mutex
	calls          int
	res            deepgram.Result
	err            error
	opts           deepgram.Options
	tg             *fakeTG
	sawPlaceholder bool
}

func (c *countingSTT) Transcribe(_ context.Context, _ []byte, _ string, opts deepgram.Options) (deepgram.Result, error) {
	c.mu.Lock()
	c.calls++
	c.opts = opts
	if c.tg != nil {
		c.tg.mu.Lock()
		c.sawPlaceholder = len(c.tg.ephemeral) > 0
		c.tg.mu.Unlock()
	}
	res, err := c.res, c.err
	c.mu.Unlock()
	return res, err
}

func logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func parseUpd(t *testing.T, raw string) telegram.Update {
	t.Helper()
	var u telegram.Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestHandleCacheHitDoesNotCallListen(t *testing.T) {
	st := &fakeStore{cached: map[string]db.Cached{
		"VOICE1\x00" + settings.DefaultVariant: {FileID: "VOICE1", Transcript: "cached hello", Language: "en", WordCount: 2},
	}}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "should-not-run"}}
	b := New(st, tg, stt, logger())
	b.self = "voicetextbot"
	upd := parseUpd(t, `{
	  "update_id": 10,
	  "message": {
	    "message_id": 1,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "VOICE1", "file_unique_id": "U1", "duration": 3}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 0 {
		t.Fatalf("Listen called %d times on cache hit", stt.calls)
	}
	if tg.downloaded != 0 || tg.gotFile != 0 {
		t.Fatal("should not download on cache hit")
	}
	got := tg.transcriptParts()
	if len(got) != 1 || !contains(got[0], "cached hello") {
		t.Fatalf("delivered = %v", got)
	}
	// A cache hit is instant, so it must not flash a placeholder first.
	if len(tg.sent) != 1 {
		t.Fatalf("a cache hit must not open a placeholder: %v", tg.sent)
	}
	if st.successes != 1 {
		t.Fatalf("successes = %d", st.successes)
	}
}

func TestHandleGroupVUsesPublicSend(t *testing.T) {
	st := &fakeStore{}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "group text", Language: "ru"}}
	b := New(st, tg, stt, logger())
	b.self = "voicetextbot"
	upd := parseUpd(t, `{
	  "update_id": 11,
	  "message": {
	    "message_id": 2,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/v",
	    "reply_to_message": {"message_id": 1, "voice": {"file_id": "F2", "file_unique_id": "U2", "duration": 2}}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 1 {
		t.Fatalf("stt calls = %d", stt.calls)
	}
	if len(tg.sent) != 1 {
		t.Fatalf("public=%v", tg.sent)
	}
	if len(tg.richChats) != 1 || tg.richChats[0] != -100 {
		t.Fatalf("rich chats = %v", tg.richChats)
	}
}

func TestRegisterGroupVPIsEphemeral(t *testing.T) {
	tg := &fakeTG{}
	b := New(&fakeStore{}, tg, &countingSTT{}, logger())
	if err := b.RegisterCommands(context.Background()); err != nil {
		t.Fatal(err)
	}
	var vp *telegram.BotCommand
	for i := range tg.groupCommands {
		if tg.groupCommands[i].Command == "vp" {
			vp = &tg.groupCommands[i]
		}
	}
	if vp == nil || !vp.IsEphemeral {
		t.Fatalf("group /vp must be ephemeral, got %#v", tg.groupCommands)
	}
}

func TestHandleGroupVPPlaceholderThenEdit(t *testing.T) {
	st := &fakeStore{}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "secret"}, tg: tg}
	b := New(st, tg, stt, logger())
	b.self = "voicetextbot"
	upd := parseUpd(t, `{
	  "update_id": 12,
	  "message": {
	    "message_id": 3,
	    "ephemeral_message_id": 88,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/vp",
	    "reply_to_message": {"message_id": 1, "video_note": {"file_id": "C1", "file_unique_id": "U3", "duration": 1, "length": 200}}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 1 {
		t.Fatalf("stt calls = %d", stt.calls)
	}
	if !stt.sawPlaceholder {
		t.Fatal("ephemeral placeholder must be sent before STT")
	}
	if len(tg.ephemeral) != 1 || tg.ephemeral[0] != transcript.WorkingText("en") {
		t.Fatalf("placeholder = %v", tg.ephemeral)
	}
	if len(tg.ephemeralRich) != 1 || !contains(tg.ephemeralRich[0], "secret") {
		t.Fatalf("rich edits = %v", tg.ephemeralRich)
	}
	if len(tg.calls) < 1 || tg.calls[0] != "sendEphemeral" {
		t.Fatalf("placeholder must be first telegram write, calls=%v", tg.calls)
	}
	var sawEdit bool
	for _, c := range tg.calls {
		if c == "editEphemeralRich" {
			sawEdit = true
		}
	}
	if !sawEdit {
		t.Fatalf("missing editEphemeralRich in %v", tg.calls)
	}
	if len(tg.sent) != 0 {
		t.Fatalf("a private transcript must never reach the group: %v", tg.sent)
	}
}

func TestHandleEmptyDoesNotSaveCache(t *testing.T) {
	st := &fakeStore{}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "  "}}
	b := New(st, tg, stt, logger())
	upd := parseUpd(t, `{
	  "update_id": 13,
	  "message": {
	    "message_id": 4,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "E1", "file_unique_id": "U4", "duration": 1}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(st.saved) != 0 {
		t.Fatalf("empty must not be cached: %v", st.saved)
	}
	if st.jobs[13] != "empty" {
		t.Fatalf("job status = %q", st.jobs[13])
	}
}

func TestDecideKindsMatchHandle(t *testing.T) {
	upd := parseUpd(t, `{
	  "update_id": 1,
	  "message": {
	    "message_id": 1,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "voice": {"file_id": "X", "file_unique_id": "Y"}
	  }
	}`)
	if decide.Decide(upd, "bot").Kind != decide.Ignore {
		t.Fatal("bare group voice must be ignore")
	}
}

func TestHandleStartUsesOwnerCallbacks(t *testing.T) {
	tg := &fakeTG{}
	b := New(&fakeStore{}, tg, &countingSTT{}, logger())
	upd := parseUpd(t, `{
	  "update_id": 20,
	  "message": {
	    "message_id": 5,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "text": "/start"
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(tg.sent) != 1 || !contains(tg.sent[0], "<b>Voicy</b>") || !contains(tg.sent[0], "<blockquote>") {
		t.Fatalf("start panel = %v", tg.sent)
	}
	if len(tg.markups) != 1 || !markupHas(tg.markups[0], "m:7:stats") || !markupHas(tg.markups[0], "m:7:close") {
		t.Fatalf("markup = %#v", tg.markups)
	}
}

func TestHandleCloseDeletes(t *testing.T) {
	tg := &fakeTG{}
	b := New(&fakeStore{}, tg, &countingSTT{}, logger())
	upd := parseUpd(t, `{
	  "update_id": 21,
	  "callback_query": {
	    "id": "cb1",
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "message": {"message_id": 99, "chat": {"id": 7, "type": "private"}},
	    "data": "m:7:close"
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(tg.deleted) != 1 || tg.deleted[0] != "7:99" {
		t.Fatalf("deleted = %v", tg.deleted)
	}
	if len(tg.edits) != 0 {
		t.Fatalf("close must delete, not edit: %v", tg.edits)
	}
}

func TestHandleForeignCallbackDoesNotEdit(t *testing.T) {
	tg := &fakeTG{}
	b := New(&fakeStore{}, tg, &countingSTT{}, logger())
	upd := parseUpd(t, `{
	  "update_id": 22,
	  "callback_query": {
	    "id": "cb2",
	    "from": {"id": 8, "is_bot": false, "first_name": "B", "language_code": "en"},
	    "message": {"message_id": 99, "chat": {"id": 7, "type": "private"}},
	    "data": "m:7:stats"
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(tg.edits) != 0 || len(tg.deleted) != 0 {
		t.Fatalf("foreign tap must be ignored, edits=%v deleted=%v", tg.edits, tg.deleted)
	}
	if len(tg.answered) != 1 || tg.answered[0] == "" {
		t.Fatalf("foreign tap should toast, answered=%v", tg.answered)
	}
}

func TestParseMenuCB(t *testing.T) {
	owner, action, ok := parseMenuCB("m:7:help")
	if !ok || owner != 7 || action != "help" {
		t.Fatalf("got %d %q %v", owner, action, ok)
	}
	owner, action, ok = parseMenuCB("m:7:lang:ru")
	if !ok || owner != 7 || action != "lang:ru" {
		t.Fatalf("lang callback = %d %q %v", owner, action, ok)
	}
	if _, _, ok := parseMenuCB("m:stats"); ok {
		t.Fatal("legacy owner-less callback must not parse")
	}
}

func TestHomePanelHasAllContractButtons(t *testing.T) {
	_, kb := homePanel("en", 7)
	for _, want := range []string{"m:7:lang", "m:7:stats", "m:7:set", "m:7:help", "m:7:about", "m:7:close"} {
		if !markupHas(kb, want) {
			t.Fatalf("home panel missing %s: %#v", want, kb)
		}
	}
}

func TestCallbackDataBudget(t *testing.T) {
	owner := int64(1) << 62
	markups := []*telegram.InlineKeyboardMarkup{}
	_, kb := homePanel("ru", owner)
	markups = append(markups, kb)
	_, kb = helpPanel("ru", owner)
	markups = append(markups, kb)
	_, kb = statsPanel("ru", owner, stats.EmptySnapshot(), false)
	markups = append(markups, kb)
	_, kb = aboutPanel("ru", owner)
	markups = append(markups, kb)
	_, kb = languagePanel("ru", owner)
	markups = append(markups, kb)
	_, kb = settingsPanel("ru", owner, settings.Default())
	markups = append(markups, kb)
	for _, m := range markups {
		for _, row := range m.InlineKeyboard {
			for _, btn := range row {
				if len(btn.CallbackData) > 64 {
					t.Fatalf("callback %q exceeds 64 bytes (%d)", btn.CallbackData, len(btn.CallbackData))
				}
			}
		}
	}
}

func TestLanguageCallbackSetsAndRerenders(t *testing.T) {
	st := &fakeStore{}
	tg := &fakeTG{}
	b := New(st, tg, &countingSTT{}, logger())
	upd := parseUpd(t, `{
	  "update_id": 30,
	  "callback_query": {
	    "id": "cb3",
	    "from": {"id": 7, "is_bot": false, "first_name": "A", "language_code": "en"},
	    "message": {"message_id": 99, "chat": {"id": 7, "type": "private"}},
	    "data": "m:7:lang:ru"
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if st.langs[7] != "ru" {
		t.Fatalf("language not stored: %v", st.langs)
	}
	if len(tg.edits) != 1 || !contains(tg.edits[0], "Язык") {
		t.Fatalf("panel must re-render in Russian: %v", tg.edits)
	}
	if !markupHas(tg.markups[len(tg.markups)-1], "m:7:lang:en") {
		t.Fatal("panel must still offer the other language")
	}

	// The stored choice must win over the Telegram profile hint on the
	// next update (profile says en, stored says ru).
	start := parseUpd(t, `{
	  "update_id": 31,
	  "message": {
	    "message_id": 6,
	    "from": {"id": 7, "is_bot": false, "first_name": "A", "language_code": "en"},
	    "chat": {"id": 7, "type": "private"},
	    "text": "/start"
	  }
	}`)
	if err := b.Handle(context.Background(), start); err != nil {
		t.Fatal(err)
	}
	if !contains(tg.sent[len(tg.sent)-1], "Голос в текст") {
		t.Fatalf("stored language must beat profile hint: %v", tg.sent)
	}
}

func TestSetCallbackOpensSettingsPanel(t *testing.T) {
	tg := &fakeTG{}
	b := New(&fakeStore{}, tg, &countingSTT{}, logger())
	upd := parseUpd(t, `{
	  "update_id": 32,
	  "callback_query": {
	    "id": "cb4",
	    "from": {"id": 7, "is_bot": false, "first_name": "A", "language_code": "en"},
	    "message": {"message_id": 99, "chat": {"id": 7, "type": "private"}},
	    "data": "m:7:set"
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(tg.edits) != 1 || !contains(tg.edits[0], "Settings") {
		t.Fatalf("settings panel = %v", tg.edits)
	}
	kb := tg.markups[len(tg.markups)-1]
	if !markupHas(kb, "m:7:set:diarize:1") || len(settings.Keys()) != 7 {
		t.Fatalf("settings panel markup = %#v", kb)
	}
}

func TestSetCallbackAppliesDesiredValueAndRerenders(t *testing.T) {
	st := &fakeStore{}
	tg := &fakeTG{}
	b := New(st, tg, &countingSTT{}, logger())
	upd := parseUpd(t, `{
	  "update_id": 33,
	  "callback_query": {
	    "id": "cb5",
	    "from": {"id": 7, "is_bot": false, "first_name": "A", "language_code": "en"},
	    "message": {"message_id": 99, "chat": {"id": 7, "type": "private"}},
	    "data": "m:7:set:diarize:1"
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(st.toggled) != 1 || st.toggled[0] != "diarize" {
		t.Fatalf("toggled = %v", st.toggled)
	}
	if !st.cfg(7).Diarize {
		t.Fatal("diarize must be on after toggle")
	}
	if len(tg.edits) != 1 || !contains(tg.edits[0], "Settings") {
		t.Fatalf("panel must re-render: %v", tg.edits)
	}
}

func TestUnknownToggleKeyIgnored(t *testing.T) {
	st := &fakeStore{}
	tg := &fakeTG{}
	b := New(st, tg, &countingSTT{}, logger())
	upd := parseUpd(t, `{
	  "update_id": 34,
	  "callback_query": {
	    "id": "cb6",
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "message": {"message_id": 99, "chat": {"id": 7, "type": "private"}},
	    "data": "m:7:set:bogus:1"
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(st.toggled) != 0 || len(tg.edits) != 0 {
		t.Fatalf("bogus key must be ignored: toggled=%v edits=%v", st.toggled, tg.edits)
	}
}

func TestTranscribePassesUserOptions(t *testing.T) {
	st := &fakeStore{userCfg: map[int64]settings.Settings{
		7: {SmartFormat: true, Paragraphs: true, Diarize: true, Quote: true},
	}}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "hi"}}
	b := New(st, tg, stt, logger())
	upd := parseUpd(t, `{
	  "update_id": 35,
	  "message": {
	    "message_id": 7,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "O1", "file_unique_id": "U9", "duration": 1}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if !stt.opts.Diarize {
		t.Fatalf("opts = %+v", stt.opts)
	}
	want := st.cfg(7).Variant()
	if len(st.savedVar) != 1 || st.savedVar[0] != want {
		t.Fatalf("saved variant = %v, want %q", st.savedVar, want)
	}
}

func TestCacheVariantsSeparateUsers(t *testing.T) {
	st := &fakeStore{}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "shared file"}}
	b := New(st, tg, stt, logger())
	voice := func(updateID, userID int64) telegram.Update {
		return parseUpd(t, `{
		  "update_id": `+itoa64(updateID)+`,
		  "message": {
		    "message_id": `+itoa64(updateID)+`,
		    "from": {"id": `+itoa64(userID)+`, "is_bot": false, "first_name": "U"},
		    "chat": {"id": `+itoa64(userID)+`, "type": "private"},
		    "voice": {"file_id": "SHARED", "file_unique_id": "UU", "duration": 1}
		  }
		}`)
	}
	// First user (defaults): transcribes and caches under the default variant.
	if err := b.Handle(context.Background(), voice(100, 7)); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 1 {
		t.Fatalf("calls = %d", stt.calls)
	}
	// Second user differs only in delivery settings: same variant, cache hit.
	if st.userCfg == nil {
		st.userCfg = map[int64]settings.Settings{}
	}
	st.userCfg[8] = settings.Settings{SmartFormat: true, Paragraphs: true, Quote: false}
	if err := b.Handle(context.Background(), voice(101, 8)); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 1 {
		t.Fatalf("delivery-only settings must reuse cache, calls = %d", stt.calls)
	}
	// Third user enables diarize: different variant, fresh transcription.
	st.userCfg[9] = settings.Settings{SmartFormat: true, Paragraphs: true, Diarize: true, Quote: true}
	if err := b.Handle(context.Background(), voice(102, 9)); err != nil {
		t.Fatal(err)
	}
	if stt.calls != 2 {
		t.Fatalf("diarize must fork the cache, calls = %d", stt.calls)
	}
}

func TestLongTranscriptUsesOneRichMessageWithinLimit(t *testing.T) {
	long := strings.Repeat("word ", 1000)
	st := &fakeStore{userCfg: map[int64]settings.Settings{
		7: {SmartFormat: true, Paragraphs: true},
	}}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: long}}
	b := New(st, tg, stt, logger())
	upd := parseUpd(t, `{
	  "update_id": 40,
	  "message": {
	    "message_id": 8,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "LONG1", "file_unique_id": "UL", "duration": 600}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	got := tg.transcriptParts()
	if len(got) != 1 || !contains(got[0], "word word word") {
		t.Fatalf("delivered = %d parts", len(got))
	}
}

// A long /vp result cannot be a new ephemeral message: Telegram only accepts one
// within 15 seconds of the command. It has to edit the placeholder instead.
func TestGroupVPLongEditsPlaceholderWithRichMessage(t *testing.T) {
	long := strings.Repeat("word ", 1000)
	st := &fakeStore{userCfg: map[int64]settings.Settings{
		7: {SmartFormat: true, Paragraphs: true},
	}}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: long}, tg: tg}
	b := New(st, tg, stt, logger())
	upd := parseUpd(t, `{
	  "update_id": 41,
	  "message": {
	    "message_id": 9,
	    "ephemeral_message_id": 77,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/vp",
	    "reply_to_message": {"message_id": 8, "voice": {"file_id": "LV", "file_unique_id": "UV", "duration": 500}}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(tg.ephemeralRich) != 1 || !contains(tg.ephemeralRich[0], "word word word") {
		t.Fatalf("ephemeral rich = %d", len(tg.ephemeralRich))
	}
	for _, c := range tg.calls {
		if c == "sendEphemeralRich" || c == "sendRich" {
			t.Fatalf("no new message may be sent after the 15-second window: %v", tg.calls)
		}
	}
}

func TestOverRichLimitSplitsIntoRichMessages(t *testing.T) {
	long := strings.Repeat("wörd ", 10000)
	st := &fakeStore{}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: long}}
	b := New(st, tg, stt, logger())
	upd := parseUpd(t, `{
	  "update_id": 42,
	  "message": {
	    "message_id": 10,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "XL", "file_unique_id": "UX", "duration": 700}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	got := tg.transcriptParts()
	if len(got) < 2 {
		t.Fatalf("over-limit transcript must be split, parts=%d", len(got))
	}
	for i, part := range got {
		if len([]rune(part)) > transcript.MaxRichCharacters {
			t.Fatalf("part %d has %d characters", i, len([]rune(part)))
		}
	}
}

// A group promoted to a supergroup answers the send with the new chat id.
// Retrying there is the difference between a delivered transcript and a job
// that burns its three retries and dies.
func TestSendRetriesAfterChatMigration(t *testing.T) {
	st := &fakeStore{}
	tg := &fakeTG{richErr: &telegram.APIError{
		Method: "sendRichMessage", StatusCode: 400, ErrorCode: 400,
		Description:     "Bad Request: group chat was upgraded to a supergroup chat",
		MigrateToChatID: -1001,
	}}
	stt := &countingSTT{res: deepgram.Result{Text: "migrated text"}}
	b := New(st, tg, stt, logger())
	b.self = "voicetextbot"
	upd := parseUpd(t, `{
	  "update_id": 43,
	  "message": {
	    "message_id": 11,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "group"},
	    "text": "/v",
	    "reply_to_message": {"message_id": 10, "voice": {"file_id": "MIG", "file_unique_id": "UM", "duration": 2}}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if got := tg.transcriptParts(); len(got) != 1 || !contains(got[0], "migrated text") {
		t.Fatalf("delivered = %v", got)
	}
	if len(tg.richChats) != 1 || tg.richChats[0] != -1001 {
		t.Fatalf("resend must target the migrated chat, got %v", tg.richChats)
	}
	if st.jobs[43] != "sent" {
		t.Fatalf("job status = %q", st.jobs[43])
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func markupHas(m *telegram.InlineKeyboardMarkup, data string) bool {
	if m == nil {
		return false
	}
	for _, row := range m.InlineKeyboard {
		for _, btn := range row {
			if btn.CallbackData == data {
				return true
			}
		}
	}
	return false
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Delivery and completion are separate writes. When the database fails between
// them the update is retried, and without the delivered_at guard the user gets
// the same transcript twice.
func TestRetryAfterDeliveryDoesNotResend(t *testing.T) {
	st := &fakeStore{}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "only once", WordCount: 2}}
	b := New(st, tg, stt, logger())
	b.self = "voicetextbot"
	upd := parseUpd(t, `{
	  "update_id": 70,
	  "message": {
	    "message_id": 20,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": -100, "type": "supergroup"},
	    "text": "/v",
	    "reply_to_message": {"message_id": 19, "voice": {"file_id": "DUP", "file_unique_id": "UDUP", "duration": 4}}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	// Simulate the completion write having been lost: the job is still received
	// but delivery was already recorded.
	st.mu.Lock()
	st.jobs[70] = "received"
	st.mu.Unlock()

	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if got := tg.transcriptParts(); len(got) != 1 {
		t.Fatalf("the retry resent the transcript: %v", got)
	}
	if stt.calls != 1 {
		t.Fatalf("the retry called Deepgram again, calls = %d", stt.calls)
	}
	st.mu.Lock()
	status := st.jobs[70]
	st.mu.Unlock()
	if status != "sent" {
		t.Fatalf("the retry must still close the job, status = %q", status)
	}
}

// A job whose transcript was delivered but never completed must not be retried
// as if nothing happened once it reaches a terminal state.
func TestTerminalJobIsNeverReprocessed(t *testing.T) {
	st := &fakeStore{jobs: map[int64]string{71: "sent"}}
	tg := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "x"}}
	b := New(st, tg, stt, logger())
	b.self = "voicetextbot"
	upd := parseUpd(t, `{
	  "update_id": 71,
	  "message": {
	    "message_id": 21,
	    "from": {"id": 7, "is_bot": false, "first_name": "A"},
	    "chat": {"id": 7, "type": "private"},
	    "voice": {"file_id": "TERM", "file_unique_id": "UT", "duration": 2}
	  }
	}`)
	if err := b.Handle(context.Background(), upd); err != nil {
		t.Fatal(err)
	}
	if len(tg.transcriptParts()) != 0 || stt.calls != 0 {
		t.Fatalf("a terminal job must be a no-op, parts=%v calls=%d", tg.transcriptParts(), stt.calls)
	}
}
