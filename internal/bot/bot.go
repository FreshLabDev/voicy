// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/FreshLabDev/voicy/internal/db"
	"github.com/FreshLabDev/voicy/internal/decide"
	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/stats"
	"github.com/FreshLabDev/voicy/internal/telegram"
	"github.com/FreshLabDev/voicy/internal/transcript"
)

type Store interface {
	Touch(context.Context, telegram.User, *telegram.Chat) error
	GetByFileID(context.Context, string) (db.Cached, bool, error)
	SaveTranscript(context.Context, string, string, string, deepgram.Result) error
	CreateJob(ctx context.Context, updateID, messageID, userID, chatID int64, kind, fileID string) (int64, string, error)
	FinishJob(ctx context.Context, id int64, status, errCode string, cacheHit bool) error
	RecordSuccess(context.Context, int64, string, deepgram.Result) error
	UserStats(context.Context, int64) (stats.Snapshot, error)
	GlobalStats(context.Context) (stats.Snapshot, error)
	Offset(context.Context) (int64, error)
	AdvanceOffset(context.Context, int64) error
}

type Telegram interface {
	GetUpdates(context.Context, int64, int) ([]telegram.Update, error)
	GetMe(context.Context) (telegram.Me, error)
	SetMyCommandsForScope(context.Context, []telegram.BotCommand, *telegram.BotCommandScope) error
	SendMessage(context.Context, int64, string, *telegram.InlineKeyboardMarkup) (telegram.Message, error)
	SendPrivateMessage(context.Context, int64, int64, string, int) (telegram.Message, error)
	SendEphemeralMessage(context.Context, int64, int64, int64, string, *telegram.InlineKeyboardMarkup) (telegram.Message, error)
	SendReply(context.Context, int64, int64, int, string) (telegram.Message, error)
	SendMessageDraft(context.Context, int64, int, string) error
	EditEphemeralMessageText(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64, text string) error
	EditMessageText(context.Context, int64, int64, string, *telegram.InlineKeyboardMarkup) error
	DeleteMessage(context.Context, int64, int64) error
	AnswerCallbackQuery(context.Context, string, string) error
	SendChatAction(context.Context, int64, string) error
	GetFile(context.Context, string) (telegram.File, error)
	DownloadFile(context.Context, string) ([]byte, error)
}

type STT interface {
	Transcribe(ctx context.Context, audio []byte, contentType string, onPartial func(string)) (deepgram.Result, error)
}

type Bot struct {
	store    Store
	tg       Telegram
	stt      STT
	log      *slog.Logger
	self     string
	lastPoll atomic.Int64
}

func New(store Store, tg Telegram, stt STT, log *slog.Logger) *Bot {
	return &Bot{store: store, tg: tg, stt: stt, log: log}
}

func (b *Bot) LastPoll() time.Time {
	unix := b.lastPoll.Load()
	if unix == 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0)
}

func (b *Bot) RegisterCommands(ctx context.Context) error {
	start := telegram.BotCommand{Command: "start", Description: "Open the menu"}
	if err := b.tg.SetMyCommandsForScope(ctx, []telegram.BotCommand{
		start,
		{Command: "stats", Description: "Your stats"},
		{Command: "help", Description: "How it works"},
	}, &telegram.BotCommandScope{Type: "all_private_chats"}); err != nil {
		return err
	}
	return b.tg.SetMyCommandsForScope(ctx, []telegram.BotCommand{
		{Command: "start", Description: "Open Voicy privately", IsEphemeral: true},
		{Command: "v", Description: "Transcribe for everyone"},
		{Command: "vp", Description: "Transcribe just for you", IsEphemeral: true},
	}, &telegram.BotCommandScope{Type: "all_group_chats"})
}

func (b *Bot) ResolveSelf(ctx context.Context) {
	me, err := b.tg.GetMe(ctx)
	if err != nil {
		b.log.Warn("getMe failed", "error", err)
		return
	}
	b.self = me.Username
}

func (b *Bot) Run(ctx context.Context) error {
	b.ResolveSelf(ctx)
	if err := b.RegisterCommands(ctx); err != nil {
		b.log.Warn("set commands failed", "error", err)
	}
	offset, err := b.store.Offset(ctx)
	if err != nil {
		return err
	}
	var pollFailures int
	for ctx.Err() == nil {
		updates, err := b.tg.GetUpdates(ctx, offset, 25)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			pollFailures++
			delay := time.Duration(pollFailures) * time.Second
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			if pollFailures <= 3 || pollFailures%10 == 0 {
				b.log.Error("telegram polling failed", "error", err, "retry_in", delay)
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(delay):
			}
			continue
		}
		pollFailures = 0
		b.lastPoll.Store(time.Now().Unix())
		for _, upd := range updates {
			if err := b.Handle(ctx, upd); err != nil {
				b.log.Error("update failed", "update_id", upd.UpdateID, "error", err)
			}
			offset = upd.UpdateID + 1
			if err := b.store.AdvanceOffset(ctx, offset); err != nil {
				return err
			}
		}
	}
	return nil
}

// Handle is the shipped entry point for one Telegram update.
func (b *Bot) Handle(ctx context.Context, upd telegram.Update) error {
	act := decide.Decide(upd, b.self)
	if act.Kind == decide.Ignore {
		return nil
	}
	if act.User.ID != 0 {
		chat := act.Chat
		if err := b.store.Touch(ctx, act.User, &chat); err != nil {
			if act.Kind == decide.Start && act.Ephemeral {
				b.log.Warn("core touch failed after deciding start", "error", err)
			} else {
				return err
			}
		}
	}
	lang := transcript.LangOf(act.LanguageCode)
	switch act.Kind {
	case decide.Start:
		return b.handleStart(ctx, act, lang)
	case decide.Help:
		text, markup := helpPanel(lang, act.UserID)
		_, err := b.tg.SendMessage(ctx, act.ChatID, text, markup)
		return err
	case decide.Stats:
		return b.handleStats(ctx, act, lang)
	case decide.Nudge:
		_, err := b.tg.SendMessage(ctx, act.ChatID, transcript.NudgeText(lang), nil)
		return err
	case decide.Callback:
		return b.handleCallback(ctx, act, lang)
	case decide.Transcribe:
		return b.transcribe(ctx, act, lang)
	default:
		return nil
	}
}

func (b *Bot) handleStart(ctx context.Context, act decide.Action, lang string) error {
	text, markup := homePanel(lang, act.UserID)
	if act.Ephemeral {
		_, err := b.tg.SendEphemeralMessage(ctx, act.ChatID, act.UserID, act.EphemeralMessageID, text, markup)
		return err
	}
	_, err := b.tg.SendMessage(ctx, act.ChatID, text, markup)
	return err
}

func (b *Bot) handleStats(ctx context.Context, act decide.Action, lang string) error {
	snap, err := b.store.UserStats(ctx, act.UserID)
	if err != nil {
		return err
	}
	text, markup := statsPanel(lang, act.UserID, snap)
	_, err = b.tg.SendMessage(ctx, act.ChatID, text, markup)
	return err
}

func (b *Bot) handleCallback(ctx context.Context, act decide.Action, lang string) error {
	owner, action, ok := parseMenuCB(act.CallbackData)
	if !ok {
		return b.tg.AnswerCallbackQuery(ctx, act.CallbackID, "")
	}
	if act.UserID != owner {
		return b.tg.AnswerCallbackQuery(ctx, act.CallbackID, transcript.NotYoursText(lang))
	}
	if err := b.tg.AnswerCallbackQuery(ctx, act.CallbackID, ""); err != nil {
		return err
	}
	switch action {
	case "home":
		text, markup := homePanel(lang, owner)
		return b.tg.EditMessageText(ctx, act.ChatID, act.CallbackMessageID, text, markup)
	case "help":
		text, markup := helpPanel(lang, owner)
		return b.tg.EditMessageText(ctx, act.ChatID, act.CallbackMessageID, text, markup)
	case "stats":
		snap, err := b.store.UserStats(ctx, act.UserID)
		if err != nil {
			return err
		}
		text, markup := statsPanel(lang, owner, snap)
		return b.tg.EditMessageText(ctx, act.ChatID, act.CallbackMessageID, text, markup)
	case "close":
		return b.tg.DeleteMessage(ctx, act.ChatID, act.CallbackMessageID)
	default:
		return nil
	}
}

func (b *Bot) transcribe(ctx context.Context, act decide.Action, lang string) error {
	if act.Media == nil || act.Media.FileID == "" {
		return nil
	}
	// /vp is ephemeral: claim the 15s reply window with a placeholder, then
	// edit after STT. A late sendMessage(receiver_user_id) is not enough.
	placeholderID, placeholderOK := b.openEphemeralPlaceholder(ctx, act, lang)
	_ = b.tg.SendChatAction(ctx, act.ChatID, "typing")

	jobID, status, err := b.store.CreateJob(ctx, act.UpdateID, act.MessageID, act.UserID, act.ChatID, act.Media.Kind, act.Media.FileID)
	if err != nil {
		return err
	}
	if status == "sent" {
		return nil
	}

	if cached, ok, err := b.store.GetByFileID(ctx, act.Media.FileID); err != nil {
		return err
	} else if ok {
		res := deepgram.Result{
			Text: cached.Transcript, Language: cached.Language, Confidence: cached.Confidence,
			Duration: cached.Duration, RequestID: cached.RequestID, WordCount: cached.WordCount,
		}
		if err := b.deliver(ctx, act, lang, res, placeholderID, placeholderOK); err != nil {
			return err
		}
		_ = b.store.FinishJob(ctx, jobID, "sent", "", true)
		return b.store.RecordSuccess(ctx, act.UserID, act.Media.Kind, res)
	}

	file, err := b.tg.GetFile(ctx, act.Media.FileID)
	if err != nil {
		b.log.Error("getFile failed", "file_id", act.Media.FileID, "update_id", act.UpdateID, "error", err)
		_ = b.store.FinishJob(ctx, jobID, "failed", "get_file", false)
		return b.deliverError(ctx, act, lang, placeholderID, placeholderOK)
	}
	audio, err := b.tg.DownloadFile(ctx, file.FilePath)
	if err != nil {
		b.log.Error("download failed", "file_id", act.Media.FileID, "update_id", act.UpdateID, "error", err)
		_ = b.store.FinishJob(ctx, jobID, "failed", "download", false)
		return b.deliverError(ctx, act, lang, placeholderID, placeholderOK)
	}
	ctype := act.Media.MimeType
	if ctype == "" && act.Media.Kind == "video_note" {
		ctype = "video/mp4"
	}
	if ctype == "" {
		ctype = "audio/ogg"
	}

	draftID := int(act.UpdateID%1_000_000_000) + 1
	res, err := b.stt.Transcribe(ctx, audio, ctype, func(partial string) {
		_ = b.tg.SendMessageDraft(ctx, act.ChatID, draftID, partial)
	})
	if err != nil {
		b.log.Error("deepgram failed", "file_id", act.Media.FileID, "update_id", act.UpdateID, "error", err)
		_ = b.store.FinishJob(ctx, jobID, "failed", "deepgram", false)
		return b.deliverError(ctx, act, lang, placeholderID, placeholderOK)
	}
	if strings.TrimSpace(res.Text) == "" {
		b.log.Info("empty transcript", "file_id", act.Media.FileID, "update_id", act.UpdateID)
		_ = b.store.FinishJob(ctx, jobID, "empty", "empty", false)
		return b.deliver(ctx, act, lang, res, placeholderID, placeholderOK)
	}
	if err := b.store.SaveTranscript(ctx, act.Media.FileID, act.Media.FileUniqueID, act.Media.Kind, res); err != nil {
		return err
	}
	if err := b.deliver(ctx, act, lang, res, placeholderID, placeholderOK); err != nil {
		return err
	}
	if err := b.store.FinishJob(ctx, jobID, "sent", "", false); err != nil {
		return err
	}
	return b.store.RecordSuccess(ctx, act.UserID, act.Media.Kind, res)
}

func (b *Bot) openEphemeralPlaceholder(ctx context.Context, act decide.Action, lang string) (int64, bool) {
	if act.Visibility != decide.Private || act.Chat.Type == "private" || act.EphemeralMessageID == 0 {
		return 0, false
	}
	replyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	msg, err := b.tg.SendEphemeralMessage(replyCtx, act.ChatID, act.UserID, act.EphemeralMessageID, transcript.WorkingText(lang), nil)
	cancel()
	if err != nil {
		b.log.Warn("ephemeral placeholder failed; will DM the result", "error", err)
		return 0, false
	}
	if msg.EphemeralMessageID != 0 {
		return msg.EphemeralMessageID, true
	}
	if msg.MessageID != 0 {
		return msg.MessageID, true
	}
	return act.EphemeralMessageID, true
}

func (b *Bot) finishEphemeral(ctx context.Context, act decide.Action, placeholderID int64, text string) error {
	if err := b.tg.EditEphemeralMessageText(ctx, act.ChatID, act.UserID, placeholderID, text); err == nil {
		return nil
	} else {
		b.log.Warn("edit ephemeral failed; falling back to DM", "error", err)
	}
	_, err := b.tg.SendMessage(ctx, act.UserID, text, nil)
	return err
}

func (b *Bot) deliver(ctx context.Context, act decide.Action, lang string, res deepgram.Result, placeholderID int64, placeholderOK bool) error {
	text := transcript.Format(res, lang)
	if placeholderOK {
		return b.finishEphemeral(ctx, act, placeholderID, text)
	}
	if act.Visibility == decide.Private && act.Chat.Type != "private" {
		_, err := b.tg.SendMessage(ctx, act.UserID, text, nil)
		return err
	}
	_, err := b.tg.SendReply(ctx, act.ChatID, act.ReplyToID, act.ThreadID, text)
	return err
}

func (b *Bot) deliverError(ctx context.Context, act decide.Action, lang string, placeholderID int64, placeholderOK bool) error {
	text := transcript.ErrorText(lang)
	if placeholderOK {
		return b.finishEphemeral(ctx, act, placeholderID, text)
	}
	if act.Visibility == decide.Private && act.Chat.Type != "private" {
		_, err := b.tg.SendMessage(ctx, act.UserID, text, nil)
		return err
	}
	_, err := b.tg.SendReply(ctx, act.ChatID, act.ReplyToID, act.ThreadID, text)
	return err
}


