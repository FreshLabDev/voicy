// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/FreshLabDev/voicy/internal/db"
	"github.com/FreshLabDev/voicy/internal/decide"
	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/settings"
	"github.com/FreshLabDev/voicy/internal/stats"
	"github.com/FreshLabDev/voicy/internal/telegram"
	"github.com/FreshLabDev/voicy/internal/transcript"
)

const (
	maxUpdateRetries       = 3
	maxEphemeralCharacters = 4096
	defaultMaxMediaBytes   = int64(20 << 20)
	defaultMaxMediaSeconds = 3600
)

type Store interface {
	Touch(context.Context, telegram.User, *telegram.Chat) error
	GetCached(ctx context.Context, fileID, variant string) (db.Cached, bool, error)
	TranscriptByToken(ctx context.Context, userID int64, token string) (db.Cached, bool, error)
	SaveTranscript(ctx context.Context, fileID, uniqueID, kind, variant string, res deepgram.Result) error
	CreateJob(ctx context.Context, updateID, messageID, userID, chatID int64, kind, fileID, variant, retrievalToken string) (db.Job, error)
	CompleteJob(ctx context.Context, id int64, status, errCode string, cacheHit bool, res deepgram.Result) error
	FailUpdate(ctx context.Context, updateID int64, errCode string) error
	UserSettings(ctx context.Context, userID int64) (settings.Settings, error)
	SetSetting(ctx context.Context, userID int64, key string, value bool) (settings.Settings, error)
	SetLanguage(ctx context.Context, userID int64, lang string) error
	EffectiveLanguage(ctx context.Context, userID int64) (string, bool, error)
	UserStats(context.Context, int64) (stats.Snapshot, error)
	GlobalStats(context.Context) (stats.Snapshot, error)
	Offset(context.Context) (int64, error)
	AdvanceOffset(context.Context, int64) error
}

type Telegram interface {
	DeleteWebhook(context.Context) error
	GetUpdates(context.Context, int64, int) ([]telegram.Update, error)
	GetMe(context.Context) (telegram.Me, error)
	SetMyCommandsForScope(context.Context, []telegram.BotCommand, *telegram.BotCommandScope) error
	SendMessage(context.Context, int64, string, *telegram.InlineKeyboardMarkup) (telegram.Message, error)
	SendEphemeralMessage(context.Context, int64, int64, int64, string, *telegram.InlineKeyboardMarkup) (telegram.Message, error)
	SendRichMarkdown(context.Context, int64, int64, int, string, *telegram.InlineKeyboardMarkup) (telegram.Message, error)
	SendEphemeralRichMarkdown(context.Context, int64, int64, int, string) (telegram.Message, error)
	EditEphemeralMessageText(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64, text string, markup *telegram.InlineKeyboardMarkup) error
	EditMessageText(context.Context, int64, int64, string, *telegram.InlineKeyboardMarkup) error
	DeleteMessage(context.Context, int64, int64) error
	DeleteEphemeralMessage(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64) error
	AnswerCallbackQuery(context.Context, string, string) error
	SendChatAction(context.Context, int64, string) error
	GetFile(context.Context, string) (telegram.File, error)
	DownloadFile(context.Context, string, int64) ([]byte, error)
}

type STT interface {
	Transcribe(ctx context.Context, audio []byte, contentType string, opts deepgram.Options) (deepgram.Result, error)
}

type Bot struct {
	store    Store
	tg       Telegram
	stt      STT
	log      *slog.Logger
	self     string
	lastPoll atomic.Int64
	ready    atomic.Bool
	maxBytes int64
	maxSecs  int
}

func New(store Store, tg Telegram, stt STT, log *slog.Logger) *Bot {
	return &Bot{store: store, tg: tg, stt: stt, log: log, maxBytes: defaultMaxMediaBytes, maxSecs: defaultMaxMediaSeconds}
}

func (b *Bot) SetMediaLimits(maxBytes int64, maxDuration time.Duration) {
	if maxBytes > 0 {
		b.maxBytes = maxBytes
	}
	if maxDuration > 0 {
		b.maxSecs = int(maxDuration.Seconds())
	}
}

func (b *Bot) Initialized() bool { return b.ready.Load() }

func (b *Bot) LastPoll() time.Time {
	unix := b.lastPoll.Load()
	if unix == 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0)
}

func (b *Bot) RegisterCommands(ctx context.Context) error {
	if err := b.tg.SetMyCommandsForScope(ctx, []telegram.BotCommand{
		{Command: "start", Description: "Open Voicy"},
		{Command: "stats", Description: "Your stats"},
		{Command: "language", Description: "Interface language"},
		{Command: "help", Description: "How it works"},
		{Command: "about", Description: "About Voicy"},
	}, &telegram.BotCommandScope{Type: "all_private_chats"}); err != nil {
		return err
	}
	return b.tg.SetMyCommandsForScope(ctx, []telegram.BotCommand{
		{Command: "start", Description: "Open Voicy privately", IsEphemeral: true},
		{Command: "v", Description: "Transcribe for everyone"},
		{Command: "vp", Description: "Transcribe just for you", IsEphemeral: true},
	}, &telegram.BotCommandScope{Type: "all_group_chats"})
}

func (b *Bot) initialize(ctx context.Context) error {
	webhookDeleted := false
	for attempt := 1; ctx.Err() == nil; attempt++ {
		if !webhookDeleted {
			if err := b.tg.DeleteWebhook(ctx); err != nil {
				b.log.Warn("telegram initialization failed", "step", "deleteWebhook", "attempt", attempt, "error", err)
				if !waitContext(ctx, retryDelay(attempt)) {
					return ctx.Err()
				}
				continue
			}
			webhookDeleted = true
		}
		me, err := b.tg.GetMe(ctx)
		if err != nil || me.Username == "" {
			if err == nil {
				err = fmt.Errorf("telegram getMe returned empty username")
			}
			b.log.Warn("telegram initialization failed", "step", "getMe", "attempt", attempt, "error", err)
			if !waitContext(ctx, retryDelay(attempt)) {
				return ctx.Err()
			}
			continue
		}
		b.self = me.Username
		if err := b.RegisterCommands(ctx); err != nil {
			b.log.Warn("telegram initialization failed", "step", "setMyCommands", "attempt", attempt, "error", err)
			if !waitContext(ctx, retryDelay(attempt)) {
				return ctx.Err()
			}
			continue
		}
		b.ready.Store(true)
		b.log.Info("telegram initialized", "username", b.self)
		return nil
	}
	return ctx.Err()
}

func (b *Bot) Run(ctx context.Context) error {
	if err := b.initialize(ctx); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
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
		if pollFailures > 0 {
			b.log.Info("telegram polling recovered", "after_failures", pollFailures)
			pollFailures = 0
		}
		b.lastPoll.Store(time.Now().Unix())
		for _, upd := range updates {
			var handleErr error
			for attempt := 1; attempt <= maxUpdateRetries; attempt++ {
				handleErr = b.Handle(ctx, upd)
				if handleErr == nil {
					break
				}
				if ctx.Err() != nil {
					return nil
				}
				if attempt < maxUpdateRetries {
					b.log.Error("telegram update failed; will retry", "update_id", upd.UpdateID, "attempt", attempt, "error", handleErr)
					if !waitContext(ctx, jitterDuration(250*time.Millisecond)) {
						return nil
					}
				}
			}
			if handleErr != nil {
				b.log.Error("telegram update permanently failed; dropping", "update_id", upd.UpdateID, "attempts", maxUpdateRetries, "error", handleErr)
				if err := b.store.FailUpdate(ctx, upd.UpdateID, "retry_exhausted"); err != nil {
					return err
				}
			}
			offset = upd.UpdateID + 1
			if err := b.store.AdvanceOffset(ctx, offset); err != nil {
				return err
			}
		}
	}
	return nil
}

func retryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}

func jitterDuration(d time.Duration) time.Duration {
	return d
}

func waitContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// Handle is the shipped entry point for one Telegram update.
func (b *Bot) Handle(ctx context.Context, upd telegram.Update) error {
	act := decide.Decide(upd, b.self)
	if act.Kind == decide.Ignore {
		return nil
	}
	if act.Kind == decide.Callback {
		owner, _, ok := parseMenuCB(act.CallbackData)
		if !ok {
			return b.tg.AnswerCallbackQuery(ctx, act.CallbackID, "")
		}
		if act.UserID != owner {
			return b.tg.AnswerCallbackQuery(ctx, act.CallbackID, transcript.NotYoursText(transcript.LangOf(act.LanguageCode)))
		}
		if err := b.tg.AnswerCallbackQuery(ctx, act.CallbackID, ""); err != nil {
			b.log.Warn("answerCallbackQuery failed", "error", err)
		}
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
	lang := b.resolveLang(ctx, act)
	switch act.Kind {
	case decide.Start:
		return b.handleStart(ctx, act, lang)
	case decide.Help:
		text, markup := helpPanel(lang, act.UserID)
		_, err := b.tg.SendMessage(ctx, act.ChatID, text, markup)
		return err
	case decide.About:
		text, markup := aboutPanel(lang, act.UserID)
		_, err := b.tg.SendMessage(ctx, act.ChatID, text, markup)
		return err
	case decide.Stats:
		return b.handleStats(ctx, act, lang, false)
	case decide.Retrieve:
		return b.handleRetrieve(ctx, act, lang)
	case decide.Nudge:
		_, err := b.tg.SendMessage(ctx, act.ChatID, transcript.NudgeText(lang), nil)
		return err
	case decide.Language:
		return b.handleLanguage(ctx, act, lang)
	case decide.Callback:
		return b.handleCallback(ctx, act, lang)
	case decide.Transcribe:
		return b.transcribe(ctx, act, lang)
	default:
		return nil
	}
}

// resolveLang prefers the manual language choice stored in the core hub
// (shared with the other bots) and falls back to the Telegram profile hint.
// The core may hold any fleet-supported language; LangOf normalizes it.
func (b *Bot) resolveLang(ctx context.Context, act decide.Action) string {
	fallback := transcript.LangOf(act.LanguageCode)
	if act.UserID == 0 {
		return fallback
	}
	eff, ok, err := b.store.EffectiveLanguage(ctx, act.UserID)
	if err != nil {
		b.log.Warn("effective language failed", "user_id", act.UserID, "error", err)
		return fallback
	}
	if !ok || eff == "" {
		return fallback
	}
	return transcript.LangOf(eff)
}

func (b *Bot) handleLanguage(ctx context.Context, act decide.Action, lang string) error {
	if code := normalizeLangChoice(act.Arg); code != "" {
		if err := b.store.SetLanguage(ctx, act.UserID, code); err != nil {
			b.log.Warn("set language failed", "user_id", act.UserID, "error", err)
		} else {
			lang = code
		}
	}
	text, markup := languagePanel(lang, act.UserID)
	_, err := b.tg.SendMessage(ctx, act.ChatID, text, markup)
	return err
}

// normalizeLangChoice maps a /language argument or callback code to "ru"/"en".
// Empty or unrecognized input means "just open the panel".
func normalizeLangChoice(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexByte(s, '-'); i > 0 {
		s = s[:i]
	}
	switch s {
	case "":
		return ""
	case "ru", "uk", "be", "en":
		return transcript.LangOf(s)
	}
	return ""
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

func (b *Bot) handleStats(ctx context.Context, act decide.Action, lang string, global bool) error {
	var snap stats.Snapshot
	var err error
	if global {
		snap, err = b.store.GlobalStats(ctx)
	} else {
		snap, err = b.store.UserStats(ctx, act.UserID)
	}
	if err != nil {
		return err
	}
	text, markup := statsPanel(lang, act.UserID, snap, global)
	_, err = b.tg.SendMessage(ctx, act.ChatID, text, markup)
	return err
}

func (b *Bot) handleRetrieve(ctx context.Context, act decide.Action, lang string) error {
	cached, ok, err := b.store.TranscriptByToken(ctx, act.UserID, act.Arg)
	if err != nil {
		return err
	}
	if !ok {
		return b.handleStart(ctx, act, lang)
	}
	s, err := b.store.UserSettings(ctx, act.UserID)
	if err != nil {
		return err
	}
	res := deepgram.Result{
		Text: cached.Transcript, Language: cached.Language, Confidence: cached.Confidence,
		Duration: cached.Duration, RequestID: cached.RequestID, WordCount: cached.WordCount, Turns: cached.Turns,
	}
	return b.sendRichParts(ctx, act.ChatID, 0, act.ThreadID, transcript.RichParts(res, lang, s))
}

func (b *Bot) handleCallback(ctx context.Context, act decide.Action, lang string) error {
	owner, action, ok := parseMenuCB(act.CallbackData)
	if !ok {
		return nil
	}
	if act.UserID != owner {
		return nil
	}
	switch action {
	case "home":
		text, markup := homePanel(lang, owner)
		return b.editPanel(ctx, act, text, markup)
	case "help":
		text, markup := helpPanel(lang, owner)
		return b.editPanel(ctx, act, text, markup)
	case "stats":
		snap, err := b.store.UserStats(ctx, act.UserID)
		if err != nil {
			return err
		}
		text, markup := statsPanel(lang, owner, snap, false)
		return b.editPanel(ctx, act, text, markup)
	case "statsp", "statsg":
		global := action == "statsg"
		var snap stats.Snapshot
		var err error
		if global {
			snap, err = b.store.GlobalStats(ctx)
		} else {
			snap, err = b.store.UserStats(ctx, act.UserID)
		}
		if err != nil {
			return err
		}
		text, markup := statsPanel(lang, owner, snap, global)
		return b.editPanel(ctx, act, text, markup)
	case "about":
		text, markup := aboutPanel(lang, owner)
		return b.editPanel(ctx, act, text, markup)
	case "lang":
		text, markup := languagePanel(lang, owner)
		return b.editPanel(ctx, act, text, markup)
	case "set":
		s, err := b.store.UserSettings(ctx, owner)
		if err != nil {
			return err
		}
		text, markup := settingsPanel(lang, owner, s)
		return b.editPanel(ctx, act, text, markup)
	case "close":
		return b.deletePanel(ctx, act)
	}
	if code, ok := strings.CutPrefix(action, "lang:"); ok {
		code = normalizeLangChoice(code)
		if code == "" {
			return nil
		}
		if err := b.store.SetLanguage(ctx, owner, code); err != nil {
			return err
		}
		text, markup := languagePanel(code, owner)
		return b.editPanel(ctx, act, text, markup)
	}
	if raw, ok := strings.CutPrefix(action, "set:"); ok {
		key, valueRaw, hasValue := strings.Cut(raw, ":")
		if _, valid := settings.SpecOf(key); !valid {
			return nil
		}
		if !hasValue || (valueRaw != "0" && valueRaw != "1") {
			return nil
		}
		s, err := b.store.SetSetting(ctx, owner, key, valueRaw == "1")
		if err != nil {
			return err
		}
		text, markup := settingsPanel(lang, owner, s)
		return b.editPanel(ctx, act, text, markup)
	}
	return nil
}

func (b *Bot) editPanel(ctx context.Context, act decide.Action, text string, markup *telegram.InlineKeyboardMarkup) error {
	if act.Ephemeral && act.EphemeralMessageID != 0 {
		return b.tg.EditEphemeralMessageText(ctx, act.ChatID, act.UserID, act.EphemeralMessageID, text, markup)
	}
	return b.tg.EditMessageText(ctx, act.ChatID, act.CallbackMessageID, text, markup)
}

func (b *Bot) deletePanel(ctx context.Context, act decide.Action) error {
	if act.Ephemeral && act.EphemeralMessageID != 0 {
		return b.tg.DeleteEphemeralMessage(ctx, act.ChatID, act.UserID, act.EphemeralMessageID)
	}
	return b.tg.DeleteMessage(ctx, act.ChatID, act.CallbackMessageID)
}

func (b *Bot) transcribe(ctx context.Context, act decide.Action, lang string) error {
	if act.Media == nil || act.Media.FileID == "" {
		return nil
	}
	s, err := b.store.UserSettings(ctx, act.UserID)
	if err != nil {
		return err
	}
	variant := s.Variant()
	token, err := retrievalToken()
	if err != nil {
		return err
	}
	job, err := b.store.CreateJob(ctx, act.UpdateID, act.MessageID, act.UserID, act.ChatID, act.Media.Kind, act.Media.FileID, variant, token)
	if err != nil {
		return err
	}
	if job.Status == "sent" || job.Status == "empty" || job.Status == "failed" {
		return nil
	}

	placeholderID, placeholderOK, err := b.openEphemeralPlaceholder(ctx, act, lang)
	if err != nil {
		return err
	}
	if b.mediaTooLarge(act.Media.Duration, act.Media.FileSize) {
		return b.failJob(ctx, job.ID, "media_limit", act, placeholderID, placeholderOK, transcript.TooLargeText(lang))
	}
	_ = b.tg.SendChatAction(ctx, act.ChatID, "typing")

	if cached, ok, err := b.store.GetCached(ctx, act.Media.FileID, variant); err != nil {
		return err
	} else if ok {
		res := deepgram.Result{
			Text: cached.Transcript, Language: cached.Language, Confidence: cached.Confidence,
			Duration: cached.Duration, RequestID: cached.RequestID, WordCount: cached.WordCount, Turns: cached.Turns,
		}
		if err := b.deliver(ctx, act, lang, s, res, job.RetrievalToken, placeholderID, placeholderOK); err != nil {
			return err
		}
		return b.completeJob(ctx, job.ID, "sent", "", true, res)
	}

	file, err := b.tg.GetFile(ctx, act.Media.FileID)
	if err != nil {
		b.log.Error("getFile failed", "file_id", act.Media.FileID, "update_id", act.UpdateID, "error", err)
		return b.failJob(ctx, job.ID, "get_file", act, placeholderID, placeholderOK, transcript.ErrorText(lang))
	}
	if b.mediaTooLarge(act.Media.Duration, file.FileSize) {
		return b.failJob(ctx, job.ID, "media_limit", act, placeholderID, placeholderOK, transcript.TooLargeText(lang))
	}
	audio, err := b.tg.DownloadFile(ctx, file.FilePath, b.maxBytes)
	if err != nil {
		b.log.Error("download failed", "file_id", act.Media.FileID, "update_id", act.UpdateID, "error", err)
		return b.failJob(ctx, job.ID, "download", act, placeholderID, placeholderOK, transcript.ErrorText(lang))
	}
	ctype := act.Media.MimeType
	if ctype == "" && act.Media.Kind == "video_note" {
		ctype = "video/mp4"
	}
	if ctype == "" {
		ctype = "audio/ogg"
	}

	opts := deepgram.Options{
		SmartFormat:     s.SmartFormat,
		Paragraphs:      s.Paragraphs,
		FillerWords:     s.FillerWords,
		ProfanityFilter: s.Profanity,
		Diarize:         s.Diarize,
	}
	if opts.Diarize && !opts.Paragraphs {
		// Speaker turns are returned on paragraph objects.
		opts.Paragraphs = true
	}
	res, err := b.stt.Transcribe(ctx, audio, ctype, opts)
	if err != nil {
		b.log.Error("deepgram failed", "file_id", act.Media.FileID, "update_id", act.UpdateID, "error", err)
		return b.failJob(ctx, job.ID, "deepgram", act, placeholderID, placeholderOK, transcript.ErrorText(lang))
	}
	if strings.TrimSpace(res.Text) == "" {
		b.log.Info("empty transcript", "file_id", act.Media.FileID, "update_id", act.UpdateID)
		if err := b.deliver(ctx, act, lang, s, res, job.RetrievalToken, placeholderID, placeholderOK); err != nil {
			return err
		}
		return b.completeJob(ctx, job.ID, "empty", "empty", false, res)
	}
	if err := b.store.SaveTranscript(ctx, act.Media.FileID, act.Media.FileUniqueID, act.Media.Kind, variant, res); err != nil {
		return err
	}
	if err := b.deliver(ctx, act, lang, s, res, job.RetrievalToken, placeholderID, placeholderOK); err != nil {
		return err
	}
	return b.completeJob(ctx, job.ID, "sent", "", false, res)
}

func retrievalToken() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate retrieval token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (b *Bot) mediaTooLarge(duration int, size int64) bool {
	return (duration > 0 && duration > b.maxSecs) || (size > 0 && size > b.maxBytes)
}

func (b *Bot) completeJob(ctx context.Context, id int64, status, errCode string, cacheHit bool, res deepgram.Result) error {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		err = b.store.CompleteJob(ctx, id, status, errCode, cacheHit, res)
		if err == nil {
			return nil
		}
		if attempt < 3 && !waitContext(ctx, time.Duration(attempt)*100*time.Millisecond) {
			return ctx.Err()
		}
	}
	return err
}

func (b *Bot) failJob(ctx context.Context, id int64, code string, act decide.Action, placeholderID int64, placeholderOK bool, text string) error {
	if err := b.completeJob(ctx, id, "failed", code, false, deepgram.Result{}); err != nil {
		return err
	}
	if err := b.deliverStatus(ctx, act, text, placeholderID, placeholderOK); err != nil {
		b.log.Warn("failed to deliver job status", "update_id", act.UpdateID, "error", err)
	}
	return nil
}

func (b *Bot) openEphemeralPlaceholder(ctx context.Context, act decide.Action, lang string) (int64, bool, error) {
	if act.Visibility != decide.Private || act.Chat.Type == "private" || act.EphemeralMessageID == 0 {
		return 0, false, nil
	}
	replyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	msg, err := b.tg.SendEphemeralMessage(replyCtx, act.ChatID, act.UserID, act.EphemeralMessageID, transcript.WorkingText(lang), nil)
	cancel()
	if err != nil {
		return 0, false, fmt.Errorf("open ephemeral placeholder: %w", err)
	}
	if msg.EphemeralMessageID != 0 {
		return msg.EphemeralMessageID, true, nil
	}
	return 0, false, fmt.Errorf("open ephemeral placeholder: telegram returned empty ephemeral_message_id")
}

func (b *Bot) finishEphemeral(ctx context.Context, act decide.Action, placeholderID int64, text string) error {
	return b.tg.EditEphemeralMessageText(ctx, act.ChatID, act.UserID, placeholderID, text, nil)
}

func (b *Bot) deliver(ctx context.Context, act decide.Action, lang string, s settings.Settings, res deepgram.Result, token string, placeholderID int64, placeholderOK bool) error {
	parts := transcript.RichParts(res, lang, s)
	if act.Visibility == decide.Private && act.Chat.Type != "private" {
		short := transcript.Format(res, lang, s)
		if placeholderOK && utf8.RuneCountInString(short) <= maxEphemeralCharacters {
			return b.finishEphemeral(ctx, act, placeholderID, short)
		}
		if placeholderOK && len(parts) == 1 {
			if _, err := b.tg.SendEphemeralRichMarkdown(ctx, act.ChatID, act.EphemeralMessageID, act.ThreadID, parts[0]); err == nil {
				_ = b.tg.DeleteEphemeralMessage(ctx, act.ChatID, act.UserID, placeholderID)
				return nil
			}
		}
		if err := b.sendRichParts(ctx, act.UserID, 0, 0, parts); err == nil {
			if placeholderOK {
				return b.finishEphemeral(ctx, act, placeholderID, transcript.SentPrivatelyText(lang))
			}
			return nil
		}
		if !placeholderOK || b.self == "" || token == "" {
			return fmt.Errorf("private rich transcript delivery unavailable")
		}
		return b.finishEphemeral(ctx, act, placeholderID, transcript.PrivateTranscriptLinkText(lang, b.self, token))
	}
	return b.sendRichParts(ctx, act.ChatID, act.ReplyToID, act.ThreadID, parts)
}

func (b *Bot) deliverStatus(ctx context.Context, act decide.Action, text string, placeholderID int64, placeholderOK bool) error {
	if placeholderOK {
		return b.finishEphemeral(ctx, act, placeholderID, text)
	}
	if act.Visibility == decide.Private && act.Chat.Type != "private" {
		return fmt.Errorf("ephemeral delivery unavailable")
	}
	return b.sendRichParts(ctx, act.ChatID, act.ReplyToID, act.ThreadID, []string{text})
}

func (b *Bot) sendRichParts(ctx context.Context, chatID, replyTo int64, threadID int, parts []string) error {
	for _, part := range parts {
		msg, err := b.tg.SendRichMarkdown(ctx, chatID, replyTo, threadID, part, nil)
		if err != nil {
			return err
		}
		if msg.MessageID != 0 {
			replyTo = msg.MessageID
		} else {
			replyTo = 0
		}
	}
	return nil
}
