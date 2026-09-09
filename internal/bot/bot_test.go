// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/FreshLabDev/tg"
	"github.com/FreshLabDev/voicy/internal/db"
	"github.com/FreshLabDev/voicy/internal/decide"
	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/settings"
	"github.com/FreshLabDev/voicy/internal/stats"
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

func (f *fakeStore) Touch(context.Context, tg.User, *tg.Chat) error {
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
	markups        []*tg.InlineKeyboardMarkup
	groupCommands  []tg.BotCommand
	privateCmds    []tg.BotCommand
	downloaded     int
	gotFile        int
	calls          []string
	richErr        error
	richEdits      []string
	chatActions    int
	onRich         func()
}

func (f *fakeTG) DeleteWebhook(context.Context) error                         { return nil }
func (f *fakeTG) GetUpdates(context.Context, int64, int) ([]tg.Update, error) { return nil, nil }
func (f *fakeTG) GetMe(context.Context) (tg.Me, error) {
	return tg.Me{Username: "voicetextbot"}, nil
}
func (f *fakeTG) SetMyCommandsForScope(_ context.Context, commands []tg.BotCommand, scope *tg.BotCommandScope) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if scope != nil && scope.Type == "all_group_chats" {
		f.groupCommands = append([]tg.BotCommand{}, commands...)
	}
	if scope != nil && scope.Type == "all_private_chats" {
		f.privateCmds = append([]tg.BotCommand{}, commands...)
	}
	return nil
}
func (f *fakeTG) SendMessage(_ context.Context, _ int64, text string, markup *tg.InlineKeyboardMarkup) (tg.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "sendMessage")
	f.sent = append(f.sent, text)
	f.markups = append(f.markups, markup)
	return tg.Message{}, nil
}
func (f *fakeTG) SendEphemeralMessage(_ context.Context, _, _, _ int64, text string, _ *tg.InlineKeyboardMarkup) (tg.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "sendEphemeral")
	f.ephemeral = append(f.ephemeral, text)
	return tg.Message{MessageID: 501, EphemeralMessageID: 501}, nil
}
func (f *fakeTG) SendRichHTML(_ context.Context, chatID, _ int64, _ int, text string, _ *tg.InlineKeyboardMarkup, _ ...tg.RichOption) (tg.Message, error) {
	f.mu.Lock()
	hook := f.onRich
	f.calls = append(f.calls, "sendRich")
	if f.richErr != nil {
		err := f.richErr
		f.richErr = nil
		f.mu.Unlock()
		return tg.Message{}, err
	}
	f.sent = append(f.sent, text)
	f.richChats = append(f.richChats, chatID)
	id := int64(len(f.sent))
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return tg.Message{MessageID: id}, nil
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

func (f *fakeTG) EditMessageRichHTML(_ context.Context, _, _ int64, text string, _ *tg.InlineKeyboardMarkup, _ ...tg.RichOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "editRich")
	f.richEdits = append(f.richEdits, text)
	return nil
}
func (f *fakeTG) EditEphemeralRichHTML(_ context.Context, _, _, _ int64, text string, _ *tg.InlineKeyboardMarkup, _ ...tg.RichOption) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "editEphemeralRich")
	f.ephemeralRich = append(f.ephemeralRich, text)
	return nil
}
func (f *fakeTG) EditEphemeralMessageText(_ context.Context, _, _, _ int64, text string, _ *tg.InlineKeyboardMarkup) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "editEphemeral")
	f.ephemeralEdits = append(f.ephemeralEdits, text)
	return nil
}
func (f *fakeTG) EditMessageText(_ context.Context, _, _ int64, text string, markup *tg.InlineKeyboardMarkup) error {
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
func (f *fakeTG) GetFile(context.Context, string) (tg.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotFile++
	return tg.File{FilePath: "voice/x.ogg"}, nil
}
func (f *fakeTG) DownloadToFile(_ context.Context, _, dst string, _ int64) error {
	f.mu.Lock()
	f.downloaded++
	f.mu.Unlock()
	return os.WriteFile(dst, []byte("AUDIO"), 0o600)
}

type countingSTT struct {
	mu             sync.Mutex
	calls          int
	res            deepgram.Result
	err            error
	opts           deepgram.Options
	api            *fakeTG
	sawPlaceholder bool
}

func (c *countingSTT) Transcribe(_ context.Context, _ string, _ string, opts deepgram.Options) (deepgram.Result, error) {
	c.mu.Lock()
	c.calls++
	c.opts = opts
	if c.api != nil {
		c.api.mu.Lock()
		c.sawPlaceholder = len(c.api.ephemeral) > 0
		c.api.mu.Unlock()
	}
	res, err := c.res, c.err
	c.mu.Unlock()
	return res, err
}

func logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func parseUpd(t *testing.T, raw string) tg.Update {
	t.Helper()
	var u tg.Update
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}
	return u
}

func TestHandleCacheHitDoesNotCallListen(t *testing.T) {
	st := &fakeStore{cached: map[string]db.Cached{
		"VOICE1\x00" + settings.DefaultVariant: {FileID: "VOICE1", Transcript: "cached hello", Language: "en", WordCount: 2},
	}}
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "should-not-run"}}
	b := New(st, api, stt, logger())
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
	if api.downloaded != 0 || api.gotFile != 0 {
		t.Fatal("should not download on cache hit")
	}
	got := api.transcriptParts()
	if len(got) != 1 || !contains(got[0], "cached hello") {
		t.Fatalf("delivered = %v", got)
	}
	// A cache hit is instant, so it must not flash a placeholder first.
	if len(api.sent) != 1 {
		t.Fatalf("a cache hit must not open a placeholder: %v", api.sent)
	}
	if st.successes != 1 {
		t.Fatalf("successes = %d", st.successes)
	}
}

func TestHandleGroupVUsesPublicSend(t *testing.T) {
	st := &fakeStore{}
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "group text", Language: "ru"}}
	b := New(st, api, stt, logger())
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
	if len(api.sent) != 1 {
		t.Fatalf("public=%v", api.sent)
	}
	if len(api.richChats) != 1 || api.richChats[0] != -100 {
		t.Fatalf("rich chats = %v", api.richChats)
	}
}

func TestRegisterGroupVPIsEphemeral(t *testing.T) {
	api := &fakeTG{}
	b := New(&fakeStore{}, api, &countingSTT{}, logger())
	if err := b.RegisterCommands(context.Background()); err != nil {
		t.Fatal(err)
	}
	var vp *tg.BotCommand
	for i := range api.groupCommands {
		if api.groupCommands[i].Command == "vp" {
			vp = &api.groupCommands[i]
		}
	}
	if vp == nil || !vp.IsEphemeral {
		t.Fatalf("group /vp must be ephemeral, got %#v", api.groupCommands)
	}
}

func TestHandleGroupVPPlaceholderThenEdit(t *testing.T) {
	st := &fakeStore{}
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "secret"}, api: api}
	b := New(st, api, stt, logger())
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
	if len(api.ephemeral) != 1 || api.ephemeral[0] != transcript.WorkingText("en") {
		t.Fatalf("placeholder = %v", api.ephemeral)
	}
	if len(api.ephemeralRich) != 1 || !contains(api.ephemeralRich[0], "secret") {
		t.Fatalf("rich edits = %v", api.ephemeralRich)
	}
	if len(api.calls) < 1 || api.calls[0] != "sendEphemeral" {
		t.Fatalf("placeholder must be first telegram write, calls=%v", api.calls)
	}
	var sawEdit bool
	for _, c := range api.calls {
		if c == "editEphemeralRich" {
			sawEdit = true
		}
	}
	if !sawEdit {
		t.Fatalf("missing editEphemeralRich in %v", api.calls)
	}
	if len(api.sent) != 0 {
		t.Fatalf("a private transcript must never reach the group: %v", api.sent)
	}
}

func TestHandleEmptyDoesNotSaveCache(t *testing.T) {
	st := &fakeStore{}
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "  "}}
	b := New(st, api, stt, logger())
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
	api := &fakeTG{}
	b := New(&fakeStore{}, api, &countingSTT{}, logger())
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
	if len(api.sent) != 1 || !contains(api.sent[0], "<b>Voicy</b>") || !contains(api.sent[0], "<blockquote>") {
		t.Fatalf("start panel = %v", api.sent)
	}
	if len(api.markups) != 1 || !markupHas(api.markups[0], "m:7:stats") || !markupHas(api.markups[0], "m:7:about") {
		t.Fatalf("markup = %#v", api.markups)
	}
}

func TestHandleCloseDeletes(t *testing.T) {
	api := &fakeTG{}
	b := New(&fakeStore{}, api, &countingSTT{}, logger())
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
	if len(api.deleted) != 1 || api.deleted[0] != "7:99" {
		t.Fatalf("deleted = %v", api.deleted)
	}
	if len(api.edits) != 0 {
		t.Fatalf("close must delete, not edit: %v", api.edits)
	}
}

func TestHandleForeignCallbackDoesNotEdit(t *testing.T) {
	api := &fakeTG{}
	b := New(&fakeStore{}, api, &countingSTT{}, logger())
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
	if len(api.edits) != 0 || len(api.deleted) != 0 {
		t.Fatalf("foreign tap must be ignored, edits=%v deleted=%v", api.edits, api.deleted)
	}
	if len(api.answered) != 1 || api.answered[0] == "" {
		t.Fatalf("foreign tap should toast, answered=%v", api.answered)
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
	_, kb := homePanel("en", 7, scopePrivate)
	for _, want := range []string{"m:7:lang", "m:7:stats", "m:7:set", "m:7:help", "m:7:about"} {
		if !markupHas(kb, want) {
			t.Fatalf("home panel missing %s: %#v", want, kb)
		}
	}
}

// everyPanel renders every screen in one scope, so a rule that has to hold on
// all of them can be read off one map instead of six call sites.
func everyPanel(lang string, owner int64, sc scope) map[string]*tg.InlineKeyboardMarkup {
	out := map[string]*tg.InlineKeyboardMarkup{}
	_, out["home"] = homePanel(lang, owner, sc)
	_, out["help"] = helpPanel(lang, owner, sc)
	_, out["stats personal"] = statsPanel(lang, owner, stats.EmptySnapshot(), false, sc)
	_, out["stats global"] = statsPanel(lang, owner, stats.EmptySnapshot(), true, sc)
	_, out["about"] = aboutPanel(lang, owner, "v1", sc)
	_, out["language"] = languagePanel(lang, owner, sc)
	_, out["settings"] = settingsPanel(lang, owner, settings.Default(), sc)
	return out
}

func buttonsOf(m *tg.InlineKeyboardMarkup) []tg.InlineKeyboardButton {
	var out []tg.InlineKeyboardButton
	if m == nil {
		return out
	}
	for _, row := range m.InlineKeyboard {
		out = append(out, row...)
	}
	return out
}

// A direct chat is the panel. Deleting the message the person is reading leaves
// them with their own history and no way back, so Close is a group affordance —
// and where it is offered it is painted destructive, because it destroys.
func TestCloseIsOfferedOnlyInGroupsAndIsDanger(t *testing.T) {
	// The group home is its own screen: it drops the personal tabs, so it is
	// not reachable through the panels a group never shows.
	private, group := everyPanel("en", 7, scopePrivate), everyPanel("en", 7, scopeGroup)
	for name, m := range private {
		if markupHas(m, "m:7:close") {
			t.Errorf("%s offers Close in a direct chat: %#v", name, m)
		}
	}
	for name, m := range group {
		if !markupHas(m, "m:7:close") {
			t.Errorf("%s hides Close in a group: %#v", name, m)
		}
		for _, btn := range buttonsOf(m) {
			if btn.CallbackData == "m:7:close" && btn.Style != tg.StyleDanger {
				t.Errorf("%s paints Close %q, want danger", name, btn.Style)
			}
			if btn.Style == tg.StyleDanger && btn.CallbackData != "m:7:close" {
				t.Errorf("%s paints %q destructive and it destroys nothing", name, btn.CallbackData)
			}
		}
	}
}

// Primary marks the one thing a person most likely came to do, so a screen has
// at most one and most screens have none. The whole panel is worth checking at
// once: two Primaries single out neither, and the statistics tabs used to spend
// the colour on a tab that reports which numbers are showing.
func TestPrimaryIsOnlyTheHomeLanguageButton(t *testing.T) {
	for _, sc := range []scope{scopePrivate, scopeGroup} {
		for name, m := range everyPanel("en", 7, sc) {
			var primaries []string
			for _, btn := range buttonsOf(m) {
				if btn.Style == tg.StylePrimary {
					primaries = append(primaries, btn.CallbackData)
				}
			}
			want := []string{}
			if name == "home" && sc == scopePrivate {
				want = []string{"m:7:lang"}
			}
			if len(primaries) != len(want) || (len(want) == 1 && primaries[0] != want[0]) {
				t.Errorf("%s (scope %d) has Primary on %v, want %v", name, sc, primaries, want)
			}
		}
	}
}

// Success reports the state the reader is in, so it lands on the current
// language, the open statistics tab and every switch that is on — and never on
// a button that only does something.
func TestSuccessMarksStateAndNothingElse(t *testing.T) {
	_, kb := statsPanel("en", 7, stats.EmptySnapshot(), true, scopePrivate)
	for _, btn := range buttonsOf(kb) {
		want := ""
		if btn.CallbackData == "m:7:statsg" {
			want = tg.StyleSuccess
		}
		if btn.Style != want {
			t.Errorf("global tab open: %s is %q, want %q", btn.CallbackData, btn.Style, want)
		}
		// Both tabs are one set, so the closed one shows it is closed.
		if strings.HasPrefix(btn.CallbackData, "m:7:stats") && !strings.HasPrefix(btn.Text, toggleOn) && !strings.HasPrefix(btn.Text, toggleOff) {
			t.Errorf("tab %s carries no state glyph: %q", btn.CallbackData, btn.Text)
		}
	}

	_, kb = languagePanel("ru", 7, scopePrivate)
	for _, btn := range buttonsOf(kb) {
		want := ""
		if btn.CallbackData == "m:7:lang:ru" {
			want = tg.StyleSuccess
		}
		if btn.Style != want {
			t.Errorf("Russian chosen: %s is %q, want %q", btn.CallbackData, btn.Style, want)
		}
	}

	on, err := settings.Default().Apply("diarize", true)
	if err != nil {
		t.Fatal(err)
	}
	_, kb = settingsPanel("en", 7, on, scopePrivate)
	for _, btn := range buttonsOf(kb) {
		key, _, isToggle := strings.Cut(strings.TrimPrefix(btn.CallbackData, "m:7:set:"), ":")
		if !isToggle || !strings.HasPrefix(btn.CallbackData, "m:7:set:") {
			continue
		}
		want := ""
		if on.IsOn(key) {
			want = tg.StyleSuccess
		}
		if btn.Style != want {
			t.Errorf("%s is %q, want %q", key, btn.Style, want)
		}
		if !strings.HasPrefix(btn.Text, toggleMark(on.IsOn(key))) {
			t.Errorf("%s carries the wrong glyph: %q", key, btn.Text)
		}
	}
}

// Settings and the interface language are personal and shared with the sibling
// bots. Offering them from a group would promise an effect on that group.
func TestGroupHomePanelDropsPersonalTabs(t *testing.T) {
	text, kb := homePanel("en", 7, scopeGroup)
	for _, unwanted := range []string{"m:7:set", "m:7:lang", "m:7:stats", "m:7:help"} {
		if markupHas(kb, unwanted) {
			t.Errorf("group home offers %s: %#v", unwanted, kb)
		}
	}
	for _, want := range []string{"m:7:about", "m:7:close"} {
		if !markupHas(kb, want) {
			t.Errorf("group home missing %s: %#v", want, kb)
		}
	}
	if !strings.Contains(text, "/vp") {
		t.Errorf("group home must explain /vp: %s", text)
	}
}

// Four of the five published direct-chat commands were tabs of this same panel.
func TestPrivateCommandMenuIsStartOnly(t *testing.T) {
	api := &fakeTG{}
	b := New(&fakeStore{}, api, &countingSTT{}, logger())
	if err := b.RegisterCommands(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(api.privateCmds) != 1 || api.privateCmds[0].Command != "start" {
		t.Fatalf("direct-chat menu = %#v", api.privateCmds)
	}
}

// The card names the running build and links the repository in the text. A
// button to the same address would be the same door listed twice.
func TestAboutCardCarriesVersionAndSource(t *testing.T) {
	text, kb := aboutPanel("ru", 7, "v0.0.1-beta.5", scopePrivate)
	for _, want := range []string{"<b>Voicy</b>", "v0.0.1-beta.5", "github.com/FreshLabDev/voicy", "Apache-2.0", "t.me/amtiyo", "Deepgram nova-3"} {
		if !strings.Contains(text, want) {
			t.Fatalf("about card missing %q: %s", want, text)
		}
	}
	for _, row := range kb.InlineKeyboard {
		for _, btn := range row {
			if btn.URL != "" {
				t.Fatalf("about must not repeat a link as a button: %#v", btn)
			}
		}
	}
}

func TestCallbackDataBudget(t *testing.T) {
	owner := int64(1) << 62
	markups := []*tg.InlineKeyboardMarkup{}
	_, kb := homePanel("ru", owner, scopePrivate)
	markups = append(markups, kb)
	_, kb = helpPanel("ru", owner, scopePrivate)
	markups = append(markups, kb)
	_, kb = statsPanel("ru", owner, stats.EmptySnapshot(), false, scopePrivate)
	markups = append(markups, kb)
	_, kb = aboutPanel("ru", owner, "dev", scopeGroup)
	markups = append(markups, kb)
	_, kb = languagePanel("ru", owner, scopePrivate)
	markups = append(markups, kb)
	_, kb = settingsPanel("ru", owner, settings.Default(), scopeGroup)
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
	api := &fakeTG{}
	b := New(st, api, &countingSTT{}, logger())
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
	if len(api.edits) != 1 || !contains(api.edits[0], "Язык") {
		t.Fatalf("panel must re-render in Russian: %v", api.edits)
	}
	if !markupHas(api.markups[len(api.markups)-1], "m:7:lang:en") {
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
	if !contains(api.sent[len(api.sent)-1], "Голос в текст") {
		t.Fatalf("stored language must beat profile hint: %v", api.sent)
	}
}

func TestSetCallbackOpensSettingsPanel(t *testing.T) {
	api := &fakeTG{}
	b := New(&fakeStore{}, api, &countingSTT{}, logger())
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
	if len(api.edits) != 1 || !contains(api.edits[0], "Settings") {
		t.Fatalf("settings panel = %v", api.edits)
	}
	kb := api.markups[len(api.markups)-1]
	if !markupHas(kb, "m:7:set:diarize:1") || len(settings.Keys()) != 7 {
		t.Fatalf("settings panel markup = %#v", kb)
	}
}

func TestSetCallbackAppliesDesiredValueAndRerenders(t *testing.T) {
	st := &fakeStore{}
	api := &fakeTG{}
	b := New(st, api, &countingSTT{}, logger())
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
	if len(api.edits) != 1 || !contains(api.edits[0], "Settings") {
		t.Fatalf("panel must re-render: %v", api.edits)
	}
}

func TestUnknownToggleKeyIgnored(t *testing.T) {
	st := &fakeStore{}
	api := &fakeTG{}
	b := New(st, api, &countingSTT{}, logger())
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
	if len(st.toggled) != 0 || len(api.edits) != 0 {
		t.Fatalf("bogus key must be ignored: toggled=%v edits=%v", st.toggled, api.edits)
	}
}

func TestTranscribePassesUserOptions(t *testing.T) {
	st := &fakeStore{userCfg: map[int64]settings.Settings{
		7: {SmartFormat: true, Paragraphs: true, Diarize: true, Quote: true},
	}}
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "hi"}}
	b := New(st, api, stt, logger())
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
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "shared file"}}
	b := New(st, api, stt, logger())
	voice := func(updateID, userID int64) tg.Update {
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
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: long}}
	b := New(st, api, stt, logger())
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
	got := api.transcriptParts()
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
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: long}, api: api}
	b := New(st, api, stt, logger())
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
	if len(api.ephemeralRich) != 1 || !contains(api.ephemeralRich[0], "word word word") {
		t.Fatalf("ephemeral rich = %d", len(api.ephemeralRich))
	}
	for _, c := range api.calls {
		if c == "sendEphemeralRich" || c == "sendRich" {
			t.Fatalf("no new message may be sent after the 15-second window: %v", api.calls)
		}
	}
}

func TestOverRichLimitSplitsIntoRichMessages(t *testing.T) {
	long := strings.Repeat("wörd ", 10000)
	st := &fakeStore{}
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: long}}
	b := New(st, api, stt, logger())
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
	got := api.transcriptParts()
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
	api := &fakeTG{richErr: &tg.APIError{
		Method: "sendRichMessage", StatusCode: 400, ErrorCode: 400,
		Description:     "Bad Request: group chat was upgraded to a supergroup chat",
		MigrateToChatID: -1001,
	}}
	stt := &countingSTT{res: deepgram.Result{Text: "migrated text"}}
	b := New(st, api, stt, logger())
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
	if got := api.transcriptParts(); len(got) != 1 || !contains(got[0], "migrated text") {
		t.Fatalf("delivered = %v", got)
	}
	if len(api.richChats) != 1 || api.richChats[0] != -1001 {
		t.Fatalf("resend must target the migrated chat, got %v", api.richChats)
	}
	if st.jobs[43] != "sent" {
		t.Fatalf("job status = %q", st.jobs[43])
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func markupHas(m *tg.InlineKeyboardMarkup, data string) bool {
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
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "only once", WordCount: 2}}
	b := New(st, api, stt, logger())
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
	if got := api.transcriptParts(); len(got) != 1 {
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
	api := &fakeTG{}
	stt := &countingSTT{res: deepgram.Result{Text: "x"}}
	b := New(st, api, stt, logger())
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
	if len(api.transcriptParts()) != 0 || stt.calls != 0 {
		t.Fatalf("a terminal job must be a no-op, parts=%v calls=%d", api.transcriptParts(), stt.calls)
	}
}
