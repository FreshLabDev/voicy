// SPDX-License-Identifier: Apache-2.0
package bot

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
	defaultMaxMediaBytes   = int64(20 << 20)
	defaultMaxMediaSeconds = 3600
	defaultWorkers         = 4
	defaultStatsTTL        = 5 * time.Minute

	// Telegram clears a chat action after five seconds, so a single call is
	// invisible for anything but the shortest clip.
	chatActionInterval = 4 * time.Second
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
	SendRichHTML(context.Context, int64, int64, int, string, *telegram.InlineKeyboardMarkup) (telegram.Message, error)
	EditEphemeralMessageText(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64, text string, markup *telegram.InlineKeyboardMarkup) error
	EditEphemeralRichHTML(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64, body string, markup *telegram.InlineKeyboardMarkup) error
	EditMessageText(context.Context, int64, int64, string, *telegram.InlineKeyboardMarkup) error
	EditMessageRichHTML(ctx context.Context, chatID, messageID int64, body string, markup *telegram.InlineKeyboardMarkup) error
	DeleteMessage(context.Context, int64, int64) error
	DeleteEphemeralMessage(ctx context.Context, chatID, receiverUserID, ephemeralMessageID int64) error
	AnswerCallbackQuery(context.Context, string, string) error
	SendChatAction(context.Context, int64, int, string) error
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
	workers  int
	stats    *statsCache
	// pulseEvery is how often the typing indicator is refreshed. It is a field
	// so tests can observe several ticks without sleeping for seconds.
	pulseEvery time.Duration
}

func New(store Store, tg Telegram, stt STT, log *slog.Logger) *Bot {
	return &Bot{
		store:      store,
		tg:         tg,
		stt:        stt,
		log:        log,
		maxBytes:   defaultMaxMediaBytes,
		maxSecs:    defaultMaxMediaSeconds,
		workers:    defaultWorkers,
		stats:      newStatsCache(defaultStatsTTL),
		pulseEvery: chatActionInterval,
	}
}

// SetWorkers bounds how many updates are handled at once. One transcription can
// take minutes, and handling updates one at a time made every other user in
// every other chat wait behind it.
func (b *Bot) SetWorkers(n int) {
	if n > 0 {
		b.workers = n
	}
}

func (b *Bot) SetStatsTTL(ttl time.Duration) {
	if ttl > 0 {
		b.stats = newStatsCache(ttl)
	}
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
		// Starting from 0 replays at most the updates Telegram still holds; every
		// job is keyed on update_id, so a replay is deduplicated rather than
		// duplicated. Refusing to start would be the worse failure.
		b.log.Error("telegram offset load failed; starting from the oldest pending update", "error", err)
		offset = 0
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
		if len(updates) == 0 {
			continue
		}
		b.processBatch(ctx, updates)
		if ctx.Err() != nil {
			return nil
		}
		// The offset only moves once the whole batch is done, so an update that
		// is still in flight is never confirmed to Telegram.
		offset = updates[len(updates)-1].UpdateID + 1
		if err := b.store.AdvanceOffset(ctx, offset); err != nil {
			// The in-memory offset still moves, so the loop makes progress.
			// A restart before the next successful write replays the batch,
			// which CreateJob deduplicates on update_id.
			b.log.Error("telegram offset persist failed", "offset", offset, "error", err)
		}
	}
	return nil
}

// processBatch handles one poll batch with bounded concurrency. Updates from the
// same user run in arrival order on one goroutine: two voices from one person
// must not race for the same job row, and their answers must not arrive
// reordered. Different users run in parallel up to b.workers, which is the whole
// point — a ten-minute recording no longer blocks everyone else.
//
// The batch is a barrier: it returns only when every update is finished, which
// is what makes advancing the offset afterwards safe.
func (b *Bot) processBatch(ctx context.Context, updates []telegram.Update) {
	groups := groupByUser(updates)
	if len(groups) == 1 {
		for _, upd := range groups[0] {
			if ctx.Err() != nil {
				return
			}
			b.handleWithRetry(ctx, upd)
		}
		return
	}
	slots := make(chan struct{}, b.workers)
	var wg sync.WaitGroup
	for _, group := range groups {
		wg.Add(1)
		go func(group []telegram.Update) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			for _, upd := range group {
				if ctx.Err() != nil {
					return
				}
				b.handleWithRetry(ctx, upd)
			}
		}(group)
	}
	wg.Wait()
}

// groupByUser splits a batch into per-user runs, preserving both the order of
// users and the order of each user's updates.
func groupByUser(updates []telegram.Update) [][]telegram.Update {
	index := map[int64]int{}
	var groups [][]telegram.Update
	for _, upd := range updates {
		id := updateUserID(upd)
		if at, ok := index[id]; ok {
			groups[at] = append(groups[at], upd)
			continue
		}
		index[id] = len(groups)
		groups = append(groups, []telegram.Update{upd})
	}
	return groups
}

// updateUserID identifies the person an update belongs to. Updates with no user
// share bucket zero, which keeps them ordered among themselves.
func updateUserID(upd telegram.Update) int64 {
	switch {
	case upd.Callback != nil:
		return upd.Callback.From.ID
	case upd.Message != nil && upd.Message.From != nil:
		return upd.Message.From.ID
	case upd.MyChatMember != nil:
		return upd.MyChatMember.From.ID
	}
	return 0
}

func (b *Bot) handleWithRetry(ctx context.Context, upd telegram.Update) {
	for attempt := 1; attempt <= maxUpdateRetries; attempt++ {
		err := b.Handle(ctx, upd)
		if err == nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
		if attempt < maxUpdateRetries {
			b.log.Error("telegram update failed; will retry", "update_id", upd.UpdateID, "attempt", attempt, "error", err)
			if !waitContext(ctx, jitterDuration(250*time.Millisecond)) {
				return
			}
			continue
		}
		b.log.Error("telegram update permanently failed; dropping", "update_id", upd.UpdateID, "attempts", maxUpdateRetries, "error", err)
		if err := b.store.FailUpdate(ctx, upd.UpdateID, "retry_exhausted"); err != nil {
			// The stale-job reaper closes this row later; losing the
			// bookkeeping write is not a reason to stop serving users.
			b.log.Error("failing update job failed", "update_id", upd.UpdateID, "error", err)
		}
	}
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

// jitterDuration spreads retries over +/-50% so concurrent replicas and repeated
// failures of the same update do not line up on the same instant.
func jitterDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	half := int64(d / 2)
	return d - time.Duration(half) + time.Duration(rand.Int63n(2*half+1))
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
	if act.Kind == decide.Membership {
		b.log.Info("chat membership changed", "chat_id", act.ChatID, "chat_type", act.Chat.Type, "status", act.Arg)
		return nil
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
	snap, err := b.snapshot(ctx, act.UserID, global)
	if err != nil {
		return err
	}
	text, markup := statsPanel(lang, act.UserID, snap, global)
	_, err = b.tg.SendMessage(ctx, act.ChatID, text, markup)
	return err
}

// snapshot serves the statistics panel from the in-memory cache, recomputing at
// most one snapshot per key at a time. The peak-hour histogram scans the jobs
// table, and the My/All tabs are two taps apart, so an uncached panel turned
// idle toggling into a stream of scans.
func (b *Bot) snapshot(ctx context.Context, userID int64, global bool) (stats.Snapshot, error) {
	key := userID
	if global {
		key = 0
	}
	now := time.Now()
	cached, fresh, ok := b.stats.get(key, now)
	if ok && fresh {
		return cached, nil
	}
	if !ok {
		// Nothing to show yet: compute inline, the user is waiting on it.
		snap, err := b.computeStats(ctx, userID, global)
		if err != nil {
			return stats.EmptySnapshot(), err
		}
		b.stats.put(key, snap, now)
		return snap, nil
	}
	// Stale but usable: answer now and refresh behind the reply.
	if b.stats.beginRefresh(key) {
		go func() {
			defer b.stats.endRefresh(key)
			refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			snap, err := b.computeStats(refreshCtx, userID, global)
			if err != nil {
				b.log.Warn("stats refresh failed", "global", global, "error", err)
				return
			}
			b.stats.put(key, snap, time.Now())
		}()
	}
	return cached, nil
}

func (b *Bot) computeStats(ctx context.Context, userID int64, global bool) (stats.Snapshot, error) {
	if global {
		return b.store.GlobalStats(ctx)
	}
	return b.store.UserStats(ctx, userID)
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
	case "stats", "statsp", "statsg":
		global := action == "statsg"
		snap, err := b.snapshot(ctx, act.UserID, global)
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
	raw, err := b.store.UserSettings(ctx, act.UserID)
	if err != nil {
		return err
	}
	s := raw.Normalized()
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

	// The ephemeral placeholder has to be opened before anything slow: Telegram
	// only accepts it within 15 seconds of the command.
	prog, err := b.openEphemeralPlaceholder(ctx, act, lang)
	if err != nil {
		return err
	}
	defer prog.stopPulse()
	if b.mediaTooLarge(act.Media.Duration, act.Media.FileSize) {
		return b.failJob(ctx, job.ID, "media_limit", act, prog, transcript.TooLargeText(lang))
	}

	if cached, ok, err := b.store.GetCached(ctx, act.Media.FileID, variant); err != nil {
		return err
	} else if ok {
		res := deepgram.Result{
			Text: cached.Transcript, Language: cached.Language, Confidence: cached.Confidence,
			Duration: cached.Duration, RequestID: cached.RequestID, WordCount: cached.WordCount, Turns: cached.Turns,
		}
		if err := b.deliver(ctx, act, lang, s, res, job.RetrievalToken, prog); err != nil {
			return err
		}
		return b.completeJob(ctx, job.ID, "sent", "", true, res)
	}

	// Only a cache miss is slow enough to be worth announcing, so the direct
	// chat placeholder and the typing pulse start here rather than above.
	b.openDirectPlaceholder(ctx, act, lang, prog)
	prog.startPulse(ctx, b, act)

	file, err := b.tg.GetFile(ctx, act.Media.FileID)
	if err != nil {
		b.log.Error("getFile failed", "file_id", act.Media.FileID, "update_id", act.UpdateID, "error", err)
		return b.failJob(ctx, job.ID, "get_file", act, prog, transcript.ErrorText(lang))
	}
	if b.mediaTooLarge(act.Media.Duration, file.FileSize) {
		return b.failJob(ctx, job.ID, "media_limit", act, prog, transcript.TooLargeText(lang))
	}
	audio, err := b.tg.DownloadFile(ctx, file.FilePath, b.maxBytes)
	if err != nil {
		b.log.Error("download failed", "file_id", act.Media.FileID, "update_id", act.UpdateID, "error", err)
		return b.failJob(ctx, job.ID, "download", act, prog, transcript.ErrorText(lang))
	}
	ctype := act.Media.MimeType
	if ctype == "" && act.Media.Kind == "video_note" {
		ctype = "video/mp4"
	}
	if ctype == "" {
		ctype = "audio/ogg"
	}

	// s is already normalized, so these options match the variant the transcript
	// is cached under.
	opts := deepgram.Options{
		SmartFormat:     s.SmartFormat,
		Paragraphs:      s.Paragraphs,
		FillerWords:     s.FillerWords,
		ProfanityFilter: s.Profanity,
		Diarize:         s.Diarize,
		// A client-side deadline hint only; it never reaches the Deepgram query.
		AudioSeconds: act.Media.Duration,
	}
	res, err := b.stt.Transcribe(ctx, audio, ctype, opts)
	if err != nil {
		b.log.Error("deepgram failed", "file_id", act.Media.FileID, "update_id", act.UpdateID, "kind", act.Media.Kind, "duration", act.Media.Duration, "error", err)
		return b.failJob(ctx, job.ID, "deepgram", act, prog, transcript.ErrorText(lang))
	}
	if strings.TrimSpace(res.Text) == "" {
		b.log.Info("empty transcript", "file_id", act.Media.FileID, "update_id", act.UpdateID)
		if err := b.deliver(ctx, act, lang, s, res, job.RetrievalToken, prog); err != nil {
			return err
		}
		return b.completeJob(ctx, job.ID, "empty", "empty", false, res)
	}
	if err := b.store.SaveTranscript(ctx, act.Media.FileID, act.Media.FileUniqueID, act.Media.Kind, variant, res); err != nil {
		return err
	}
	if err := b.deliver(ctx, act, lang, s, res, job.RetrievalToken, prog); err != nil {
		return err
	}
	return b.completeJob(ctx, job.ID, "sent", "", false, res)
}

func retrievalToken() (string, error) {
	raw := make([]byte, 18)
	if _, err := cryptorand.Read(raw); err != nil {
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

func (b *Bot) failJob(ctx context.Context, id int64, code string, act decide.Action, prog *progress, text string) error {
	if err := b.completeJob(ctx, id, "failed", code, false, deepgram.Result{}); err != nil {
		return err
	}
	prog.stopPulse()
	if err := b.deliverStatus(ctx, act, text, prog); err != nil {
		b.log.Warn("failed to deliver job status", "update_id", act.UpdateID, "error", err)
	}
	return nil
}

// progress is everything Voicy has already shown the user about one in-flight
// transcription: the ephemeral placeholder in a group, the ordinary placeholder
// in a direct chat, and the repeating typing indicator.
type progress struct {
	ephemeralID int64
	ephemeralOK bool
	directID    int64
	stop        context.CancelFunc
	done        chan struct{}
}

// startPulse keeps the typing indicator alive. Telegram clears a chat action
// after five seconds, so the single call this replaced was invisible for
// anything longer than a short clip.
func (p *progress) startPulse(ctx context.Context, b *Bot, act decide.Action) {
	if p.stop != nil {
		return
	}
	pulseCtx, cancel := context.WithCancel(ctx)
	p.stop = cancel
	p.done = make(chan struct{})
	go func() {
		defer close(p.done)
		ticker := time.NewTicker(b.pulseEvery)
		defer ticker.Stop()
		for {
			_ = b.tg.SendChatAction(pulseCtx, act.ChatID, act.ThreadID, "typing")
			select {
			case <-pulseCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// stopPulse ends the indicator and waits for the goroutine, so no chat action
// can land after the transcript and revive a "typing" status.
func (p *progress) stopPulse() {
	if p.stop == nil {
		return
	}
	p.stop()
	<-p.done
	p.stop = nil
}

func (b *Bot) openEphemeralPlaceholder(ctx context.Context, act decide.Action, lang string) (*progress, error) {
	prog := &progress{}
	if act.Visibility != decide.Private || act.Chat.Type == "private" || act.EphemeralMessageID == 0 {
		return prog, nil
	}
	replyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	msg, err := b.tg.SendEphemeralMessage(replyCtx, act.ChatID, act.UserID, act.EphemeralMessageID, transcript.WorkingText(lang), nil)
	cancel()
	if err != nil {
		return prog, fmt.Errorf("open ephemeral placeholder: %w", err)
	}
	if msg.EphemeralMessageID == 0 {
		return prog, fmt.Errorf("open ephemeral placeholder: telegram returned empty ephemeral_message_id")
	}
	prog.ephemeralID = msg.EphemeralMessageID
	prog.ephemeralOK = true
	return prog, nil
}

// openDirectPlaceholder answers a direct chat immediately with "Transcribing…"
// and remembers the message so the result can replace it. Failing to post it is
// not fatal: the transcript is still delivered as a new message.
func (b *Bot) openDirectPlaceholder(ctx context.Context, act decide.Action, lang string, prog *progress) {
	if act.Chat.Type != "private" || prog.ephemeralOK {
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Sent as a rich message so it replies to the voice, keeps the topic, and
	// can later be edited into the transcript in the same format.
	msg, err := b.tg.SendRichHTML(sendCtx, act.ChatID, act.ReplyToID, act.ThreadID, transcript.WorkingText(lang), nil)
	if err != nil {
		b.log.Warn("direct placeholder failed", "update_id", act.UpdateID, "error", err)
		return
	}
	prog.directID = msg.MessageID
}

func (b *Bot) finishEphemeral(ctx context.Context, act decide.Action, placeholderID int64, text string) error {
	return b.tg.EditEphemeralMessageText(ctx, act.ChatID, act.UserID, placeholderID, text, nil)
}

func (b *Bot) deliver(ctx context.Context, act decide.Action, lang string, s settings.Settings, res deepgram.Result, token string, prog *progress) error {
	prog.stopPulse()
	parts := transcript.RichParts(res, lang, s)
	if act.Visibility == decide.Private && act.Chat.Type != "private" {
		// Telegram accepts a new ephemeral message only within 15 seconds of the
		// command that triggered it, and transcription has long outlived that
		// window. Editing the placeholder is therefore the only private surface
		// left, and Bot API 10.3 lets that edit carry a full rich transcript
		// instead of the 4096 characters a plain text edit allows.
		if prog.ephemeralOK && len(parts) == 1 {
			err := b.tg.EditEphemeralRichHTML(ctx, act.ChatID, act.UserID, prog.ephemeralID, parts[0], nil)
			if err == nil {
				return nil
			}
			b.log.Warn("ephemeral rich edit failed; trying direct message", "update_id", act.UpdateID, "error", err)
		}
		if err := b.sendRichParts(ctx, act.UserID, 0, 0, parts); err == nil {
			if prog.ephemeralOK {
				return b.finishEphemeral(ctx, act, prog.ephemeralID, transcript.SentPrivatelyText(lang))
			}
			return nil
		}
		if !prog.ephemeralOK || b.self == "" || token == "" {
			return fmt.Errorf("private rich transcript delivery unavailable")
		}
		return b.finishEphemeral(ctx, act, prog.ephemeralID, transcript.PrivateTranscriptLinkText(lang, b.self, token))
	}
	// A direct chat already shows "Transcribing…": turn that message into the
	// transcript instead of leaving it above a second one.
	if prog.directID != 0 {
		err := b.tg.EditMessageRichHTML(ctx, act.ChatID, prog.directID, parts[0], nil)
		if err == nil {
			// Remaining parts chain off the message the user is already reading.
			return b.sendRichParts(ctx, act.ChatID, prog.directID, act.ThreadID, parts[1:])
		}
		b.log.Warn("direct placeholder edit failed; sending a new message", "update_id", act.UpdateID, "error", err)
		// Otherwise the transcript would arrive under a stranded "Transcribing…".
		_ = b.tg.DeleteMessage(ctx, act.ChatID, prog.directID)
	}
	return b.sendRichParts(ctx, act.ChatID, act.ReplyToID, act.ThreadID, parts)
}

func (b *Bot) deliverStatus(ctx context.Context, act decide.Action, text string, prog *progress) error {
	if prog.ephemeralOK {
		return b.finishEphemeral(ctx, act, prog.ephemeralID, text)
	}
	if act.Visibility == decide.Private && act.Chat.Type != "private" {
		return fmt.Errorf("ephemeral delivery unavailable")
	}
	if prog.directID != 0 {
		// The placeholder is a rich message, so its edits stay rich too.
		return b.tg.EditMessageRichHTML(ctx, act.ChatID, prog.directID, text, nil)
	}
	return b.sendRichParts(ctx, act.ChatID, act.ReplyToID, act.ThreadID, []string{text})
}

func (b *Bot) sendRichParts(ctx context.Context, chatID, replyTo int64, threadID int, parts []string) error {
	for _, part := range parts {
		msg, err := b.tg.SendRichHTML(ctx, chatID, replyTo, threadID, part, nil)
		if err != nil {
			// A group that turned into a supergroup mid-request answers with the
			// new chat id. The reply target and topic belong to the old chat, so
			// the retry drops both.
			var apiErr *telegram.APIError
			if errors.As(err, &apiErr) && apiErr.MigrateToChatID != 0 && apiErr.MigrateToChatID != chatID {
				b.log.Info("chat migrated; resending", "from", chatID, "to", apiErr.MigrateToChatID)
				chatID, replyTo, threadID = apiErr.MigrateToChatID, 0, 0
				msg, err = b.tg.SendRichHTML(ctx, chatID, replyTo, threadID, part, nil)
			}
		}
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
