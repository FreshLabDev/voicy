// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/FreshLabDev/voicy/internal/db"
	"github.com/FreshLabDev/voicy/internal/decide"
	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/stats"
	"github.com/FreshLabDev/voicy/internal/telegram"
	"github.com/FreshLabDev/voicy/internal/transcript"
)

type fakeStore struct {
	cached     map[string]db.Cached
	saved      []string
	jobs       map[int64]string
	successes  int
	touched    int
	offset     int64
}

func (f *fakeStore) Touch(context.Context, telegram.User, *telegram.Chat) error {
	f.touched++
	return nil
}
func (f *fakeStore) GetByFileID(_ context.Context, id string) (db.Cached, bool, error) {
	c, ok := f.cached[id]
	return c, ok, nil
}
func (f *fakeStore) SaveTranscript(_ context.Context, fileID, _, _ string, _ deepgram.Result) error {
	f.saved = append(f.saved, fileID)
	return nil
}
func (f *fakeStore) CreateJob(_ context.Context, updateID, _, _, _ int64, _, _ string) (int64, string, error) {
	if f.jobs == nil {
		f.jobs = map[int64]string{}
	}
	if st, ok := f.jobs[updateID]; ok {
		return updateID, st, nil
	}
	f.jobs[updateID] = "received"
	return updateID, "received", nil
}
func (f *fakeStore) FinishJob(_ context.Context, id int64, status, _ string, _ bool) error {
	if f.jobs == nil {
		f.jobs = map[int64]string{}
	}
	f.jobs[id] = status
	return nil
}
func (f *fakeStore) RecordSuccess(context.Context, int64, string, deepgram.Result) error {
	f.successes++
	return nil
}
func (f *fakeStore) UserStats(context.Context, int64) (stats.Snapshot, error) {
	return stats.EmptySnapshot(), nil
}
func (f *fakeStore) GlobalStats(context.Context) (stats.Snapshot, error) {
	return stats.EmptySnapshot(), nil
}
func (f *fakeStore) Offset(context.Context) (int64, error) { return f.offset, nil }
func (f *fakeStore) AdvanceOffset(_ context.Context, off int64) error {
	f.offset = off
	return nil
}

type fakeTG struct {
	sent            []string
	private         []string
	ephemeral       []string
	ephemeralEdits  []string
	drafts          []string
	edits           []string
	deleted         []string
	answered        []string
	markups         []*telegram.InlineKeyboardMarkup
	groupCommands   []telegram.BotCommand
	downloaded      int
	gotFile         int
	calls           []string
}

func (f *fakeTG) GetUpdates(context.Context, int64, int) ([]telegram.Update, error) { return nil, nil }
func (f *fakeTG) GetMe(context.Context) (telegram.Me, error) {
	return telegram.Me{Username: "voicetextbot"}, nil
}
func (f *fakeTG) SetMyCommandsForScope(_ context.Context, commands []telegram.BotCommand, scope *telegram.BotCommandScope) error {
	if scope != nil && scope.Type == "all_group_chats" {
		f.groupCommands = append([]telegram.BotCommand{}, commands...)
	}
	return nil
}
func (f *fakeTG) SendMessage(_ context.Context, _ int64, text string, markup *telegram.InlineKeyboardMarkup) (telegram.Message, error) {
	f.calls = append(f.calls, "sendMessage")
	f.sent = append(f.sent, text)
	f.markups = append(f.markups, markup)
	return telegram.Message{}, nil
}
func (f *fakeTG) SendPrivateMessage(_ context.Context, _, _ int64, text string, _ int) (telegram.Message, error) {
	f.calls = append(f.calls, "sendPrivate")
	f.private = append(f.private, text)
	return telegram.Message{}, nil
}
func (f *fakeTG) SendEphemeralMessage(_ context.Context, _, _, _ int64, text string, _ *telegram.InlineKeyboardMarkup) (telegram.Message, error) {
	f.calls = append(f.calls, "sendEphemeral")
	f.ephemeral = append(f.ephemeral, text)
	return telegram.Message{MessageID: 501, EphemeralMessageID: 501}, nil
}
func (f *fakeTG) EditEphemeralMessageText(_ context.Context, _, _, _ int64, text string) error {
	f.calls = append(f.calls, "editEphemeral")
	f.ephemeralEdits = append(f.ephemeralEdits, text)
	return nil
}
func (f *fakeTG) SendReply(_ context.Context, _, _ int64, _ int, text string) (telegram.Message, error) {
	f.sent = append(f.sent, text)
	return telegram.Message{}, nil
}
func (f *fakeTG) SendMessageDraft(_ context.Context, _ int64, _ int, text string) error {
	f.drafts = append(f.drafts, text)
	return nil
}
func (f *fakeTG) EditMessageText(_ context.Context, _, _ int64, text string, markup *telegram.InlineKeyboardMarkup) error {
	f.calls = append(f.calls, "editMessage")
	f.edits = append(f.edits, text)
	f.markups = append(f.markups, markup)
	return nil
}
func (f *fakeTG) DeleteMessage(_ context.Context, chatID, messageID int64) error {
	f.calls = append(f.calls, "deleteMessage")
	f.deleted = append(f.deleted, itoa64(chatID)+":"+itoa64(messageID))
	return nil
}
func (f *fakeTG) AnswerCallbackQuery(_ context.Context, _, text string) error {
	f.calls = append(f.calls, "answerCallback")
	f.answered = append(f.answered, text)
	return nil
}
func (f *fakeTG) SendChatAction(context.Context, int64, string) error       { return nil }
func (f *fakeTG) GetFile(context.Context, string) (telegram.File, error) {
	f.gotFile++
	return telegram.File{FilePath: "voice/x.ogg"}, nil
}
func (f *fakeTG) DownloadFile(context.Context, string) ([]byte, error) {
	f.downloaded++
	return []byte("AUDIO"), nil
}

type countingSTT struct {
	calls          int
	res            deepgram.Result
	err            error
	tg             *fakeTG
	sawPlaceholder bool
}

func (c *countingSTT) Transcribe(_ context.Context, _ []byte, _ string, onPartial func(string)) (deepgram.Result, error) {
	c.calls++
	if c.tg != nil {
		c.sawPlaceholder = len(c.tg.ephemeral) > 0
	}
	if onPartial != nil && c.res.Text != "" {
		onPartial(c.res.Text)
	}
	return c.res, c.err
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
		"VOICE1": {FileID: "VOICE1", Transcript: "cached hello", Language: "en", WordCount: 2},
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
	if len(tg.sent) != 1 || !contains(tg.sent[0], "cached hello") {
		t.Fatalf("sent = %v", tg.sent)
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
	if len(tg.sent) != 1 || len(tg.private) != 0 {
		t.Fatalf("public=%v private=%v", tg.sent, tg.private)
	}
	if len(tg.drafts) != 1 {
		t.Fatalf("expected a draft, got %v", tg.drafts)
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
	if len(tg.ephemeralEdits) != 1 || !contains(tg.ephemeralEdits[0], "secret") {
		t.Fatalf("edits = %v", tg.ephemeralEdits)
	}
	if len(tg.private) != 0 {
		t.Fatalf("must not use receiver-only sendMessage: %v", tg.private)
	}
	if len(tg.calls) < 1 || tg.calls[0] != "sendEphemeral" {
		t.Fatalf("placeholder must be first telegram write, calls=%v", tg.calls)
	}
	var sawEdit bool
	for _, c := range tg.calls {
		if c == "editEphemeral" {
			sawEdit = true
		}
	}
	if !sawEdit {
		t.Fatalf("missing editEphemeral in %v", tg.calls)
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
	if _, _, ok := parseMenuCB("m:stats"); ok {
		t.Fatal("legacy owner-less callback must not parse")
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
