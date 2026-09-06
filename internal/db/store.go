// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/settings"
	"github.com/FreshLabDev/voicy/internal/stats"
	"github.com/FreshLabDev/voicy/internal/telegram"
)

type Store struct {
	pool    *pgxpool.Pool
	statsTZ string
}

// SetStatsTimezone chooses the zone the peak-hour histogram is bucketed in.
// PostgreSQL resolves the name; an unknown zone makes the query error and the
// peak hour is simply reported as unknown.
func (s *Store) SetStatsTimezone(tz string) {
	if tz != "" {
		s.statsTZ = tz
	}
}

func (s *Store) timezone() string {
	if s.statsTZ == "" {
		return "Europe/Kyiv"
	}
	return s.statsTZ
}

func Connect(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "voicy"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Touch(ctx context.Context, user telegram.User, chat *telegram.Chat) error {
	var chatID any
	var chatType, chatTitle, chatUname any
	if chat != nil && chat.Type != "private" && chat.ID != 0 {
		chatID = chat.ID
		chatType = chat.Type
		chatTitle = nullString(chat.Title)
		chatUname = nullString(chat.Username)
	}
	_, err := s.pool.Exec(ctx,
		`SELECT core.touch($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		"voicy", user.ID, nullString(user.Username), nullString(user.FirstName),
		nullString(user.LastName), nullString(user.LanguageCode), chatID, chatType, chatTitle, chatUname, user.IsBot)
	return err
}

type Cached struct {
	FileID     string
	Kind       string
	Transcript string
	Language   string
	Confidence float64
	Duration   float64
	RequestID  string
	WordCount  int
	Turns      []deepgram.Turn
}

func (s *Store) GetCached(ctx context.Context, fileID, variant string) (Cached, bool, error) {
	var row Cached
	var turns []byte
	err := s.pool.QueryRow(ctx, `
			UPDATE transcripts SET last_used_at=now()
			WHERE file_id=$1 AND variant=$2
			RETURNING file_id, kind, transcript, COALESCE(detected_language,''), COALESCE(confidence,0),
			          COALESCE(duration_seconds,0), COALESCE(deepgram_request_id,''), word_count, speaker_turns`, fileID, variant).
		Scan(&row.FileID, &row.Kind, &row.Transcript, &row.Language, &row.Confidence, &row.Duration, &row.RequestID, &row.WordCount, &turns)
	if err == pgx.ErrNoRows {
		return Cached{}, false, nil
	}
	if err != nil {
		return Cached{}, false, err
	}
	if len(turns) > 0 {
		if err := json.Unmarshal(turns, &row.Turns); err != nil {
			return Cached{}, false, fmt.Errorf("decode cached speaker turns: %w", err)
		}
	}
	return row, true, nil
}

func (s *Store) SaveTranscript(ctx context.Context, fileID, uniqueID, kind, variant string, res deepgram.Result) error {
	turns, err := json.Marshal(res.Turns)
	if err != nil {
		return fmt.Errorf("encode speaker turns: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
			INSERT INTO transcripts (file_id, file_unique_id, kind, variant, transcript, detected_language, confidence, duration_seconds, deepgram_request_id, word_count, speaker_turns, last_used_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now())
			ON CONFLICT (file_id, variant) DO UPDATE SET
		  file_unique_id = COALESCE(EXCLUDED.file_unique_id, transcripts.file_unique_id),
		  kind = EXCLUDED.kind,
		  transcript = EXCLUDED.transcript,
		  detected_language = EXCLUDED.detected_language,
		  confidence = EXCLUDED.confidence,
		  duration_seconds = EXCLUDED.duration_seconds,
		  deepgram_request_id = EXCLUDED.deepgram_request_id,
			  word_count = EXCLUDED.word_count,
			  speaker_turns = EXCLUDED.speaker_turns,
			  last_used_at = now()`,
		fileID, nullString(uniqueID), kind, variant, res.Text, nullString(res.Language), res.Confidence, res.Duration, nullString(res.RequestID), res.WordCount, string(turns))
	return err
}

func (s *Store) UserSettings(ctx context.Context, userID int64) (settings.Settings, error) {
	out := settings.Default()
	err := s.pool.QueryRow(ctx, `
			SELECT smart_format, paragraphs, filler_words, profanity_filter, diarize, quote, meta
			FROM user_settings WHERE telegram_user_id=$1`, userID).
		Scan(&out.SmartFormat, &out.Paragraphs, &out.FillerWords, &out.Profanity, &out.Diarize, &out.Quote, &out.Meta)
	if err == pgx.ErrNoRows {
		return settings.Default(), nil
	}
	if err != nil {
		return settings.Default(), err
	}
	return out, nil
}

// SetSetting applies the value idempotently so replaying a callback cannot
// flip a toggle twice.
func (s *Store) SetSetting(ctx context.Context, userID int64, key string, value bool) (settings.Settings, error) {
	spec, ok := settings.SpecOf(key)
	if !ok {
		return settings.Default(), fmt.Errorf("unknown setting %q", key)
	}
	var out settings.Settings
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`
			INSERT INTO user_settings (telegram_user_id, %s) VALUES ($1, $2)
			ON CONFLICT (telegram_user_id) DO UPDATE SET %s = EXCLUDED.%s, updated_at = now()
			RETURNING smart_format, paragraphs, filler_words, profanity_filter, diarize, quote, meta`,
		spec.Key, spec.Key, spec.Key),
		userID, value).
		Scan(&out.SmartFormat, &out.Paragraphs, &out.FillerWords, &out.Profanity, &out.Diarize, &out.Quote, &out.Meta)
	if err != nil {
		return settings.Default(), err
	}
	return out, nil
}

// SetLanguage records a manual UI language choice in the shared core hub.
// The 'user'/'manual' literals stay inline on purpose: the production core
// takes core.pref_scope/core.lang_source enums there, and pgx would send
// bound $n parameters as text, which PostgreSQL cannot match to the enum
// overloads.
func (s *Store) SetLanguage(ctx context.Context, userID int64, lang string) error {
	_, err := s.pool.Exec(ctx, `SELECT core.set_language($1,'user',$2,$3,'manual')`, "voicy", userID, lang)
	return err
}

// EffectiveLanguage reads the resolved language from the core hub.
// Missing preference is not an error: ok=false means "use the Telegram hint".
func (s *Store) EffectiveLanguage(ctx context.Context, userID int64) (string, bool, error) {
	var lang *string
	if err := s.pool.QueryRow(ctx, `SELECT core.effective_language($1,NULL,'user')`, userID).Scan(&lang); err != nil {
		return "", false, err
	}
	if lang == nil || *lang == "" {
		return "", false, nil
	}
	return *lang, true, nil
}

type Job struct {
	ID             int64
	Status         string
	RetrievalToken string
}

func (s *Store) CreateJob(ctx context.Context, updateID, messageID, userID, chatID int64, kind, fileID, variant, retrievalToken string) (Job, error) {
	var job Job
	err := s.pool.QueryRow(ctx, `
			INSERT INTO jobs (telegram_update_id, telegram_message_id, telegram_user_id, chat_id, kind, file_id, variant, retrieval_token)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (telegram_update_id) DO UPDATE SET telegram_update_id = EXCLUDED.telegram_update_id
			RETURNING id, status, retrieval_token`, updateID, messageID, userID, chatID, kind, fileID, variant, retrievalToken).
		Scan(&job.ID, &job.Status, &job.RetrievalToken)
	return job, err
}

// CompleteJob moves a received job to a terminal state. The sent transition
// and user_stats increment share one transaction and happen at most once.
func (s *Store) CompleteJob(ctx context.Context, id int64, status, errCode string, cacheHit bool, res deepgram.Result) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID int64
	var kind string
	err = tx.QueryRow(ctx, `
		UPDATE jobs SET status=$2, error_code=$3, cache_hit=$4, finished_at=now()
		WHERE id=$1 AND status='received'
		RETURNING telegram_user_id, COALESCE(kind,'')`,
		id, status, nullString(errCode), cacheHit).Scan(&userID, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if stats.ShouldCount(status, res.Text) {
		voice, circle := 1, 0
		if kind == "video_note" {
			voice, circle = 0, 1
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_stats (telegram_user_id, transcriptions, voice_count, video_note_count, duration_seconds, words, last_language, first_at, last_at)
			VALUES ($1,1,$2,$3,$4,$5,$6,now(),now())
			ON CONFLICT (telegram_user_id) DO UPDATE SET
			  transcriptions = user_stats.transcriptions + 1,
			  voice_count = user_stats.voice_count + EXCLUDED.voice_count,
			  video_note_count = user_stats.video_note_count + EXCLUDED.video_note_count,
			  duration_seconds = user_stats.duration_seconds + EXCLUDED.duration_seconds,
			  words = user_stats.words + EXCLUDED.words,
			  last_language = COALESCE(EXCLUDED.last_language, user_stats.last_language),
			  first_at = LEAST(user_stats.first_at, EXCLUDED.first_at),
			  last_at = GREATEST(user_stats.last_at, EXCLUDED.last_at)`,
			userID, voice, circle, res.Duration, res.WordCount, nullString(res.Language)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) FailUpdate(ctx context.Context, updateID int64, errCode string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET status='failed', error_code=$2, finished_at=now()
		WHERE telegram_update_id=$1 AND status='received'`, updateID, errCode)
	return err
}

func (s *Store) UserStats(ctx context.Context, userID int64) (stats.Snapshot, error) {
	out := stats.EmptySnapshot()
	err := s.pool.QueryRow(ctx, `
			SELECT transcriptions, voice_count, video_note_count, duration_seconds, words, COALESCE(last_language,'')
		FROM user_stats WHERE telegram_user_id=$1`, userID).
		Scan(&out.Transcriptions, &out.Voice, &out.VideoNotes, &out.DurationSec, &out.Words, &out.LastLanguage)
	if err == pgx.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.PeakHour = s.peakHour(ctx, userID)
	return out, nil
}

func (s *Store) GlobalStats(ctx context.Context) (stats.Snapshot, error) {
	out := stats.EmptySnapshot()
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(transcriptions),0), COALESCE(SUM(voice_count),0), COALESCE(SUM(video_note_count),0),
		       COALESCE(SUM(duration_seconds),0), COALESCE(SUM(words),0), COUNT(*)
		FROM user_stats`).
		Scan(&out.Transcriptions, &out.Voice, &out.VideoNotes, &out.DurationSec, &out.Words, &out.Users)
	if err != nil {
		return out, err
	}
	out.PeakHour = s.peakHour(ctx, nil)
	return out, nil
}

func (s *Store) peakHour(ctx context.Context, userID any) int {
	query := `
		SELECT EXTRACT(HOUR FROM finished_at AT TIME ZONE $1)::int
		FROM jobs WHERE status='sent'`
	args := []any{s.timezone()}
	if userID != nil {
		query += ` AND telegram_user_id=$2`
		args = append(args, userID)
	}
	query += ` GROUP BY 1 ORDER BY count(*) DESC, 1 LIMIT 1`
	var hour int
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&hour); err != nil {
		return -1
	}
	return hour
}

func (s *Store) TranscriptByToken(ctx context.Context, userID int64, token string) (Cached, bool, error) {
	var row Cached
	var turns []byte
	var variant string
	err := s.pool.QueryRow(ctx, `
		SELECT t.file_id, t.kind, t.transcript, COALESCE(t.detected_language,''), COALESCE(t.confidence,0),
		       COALESCE(t.duration_seconds,0), COALESCE(t.deepgram_request_id,''), t.word_count, t.speaker_turns, t.variant
		FROM jobs j
		JOIN transcripts t ON t.file_id=j.file_id AND t.variant=j.variant
		WHERE j.telegram_user_id=$1 AND j.retrieval_token=$2 AND j.status='sent'`, userID, token).
		Scan(&row.FileID, &row.Kind, &row.Transcript, &row.Language, &row.Confidence, &row.Duration, &row.RequestID, &row.WordCount, &turns, &variant)
	if errors.Is(err, pgx.ErrNoRows) {
		return Cached{}, false, nil
	}
	if err != nil {
		return Cached{}, false, err
	}
	if len(turns) > 0 {
		if err := json.Unmarshal(turns, &row.Turns); err != nil {
			return Cached{}, false, fmt.Errorf("decode retrieved speaker turns: %w", err)
		}
	}
	_, _ = s.pool.Exec(ctx, `UPDATE transcripts SET last_used_at=now() WHERE file_id=$1 AND variant=$2`, row.FileID, variant)
	return row, true, nil
}

func (s *Store) Offset(ctx context.Context) (int64, error) {
	var off int64
	err := s.pool.QueryRow(ctx, `SELECT telegram_offset FROM runtime_state WHERE singleton`).Scan(&off)
	return off, err
}

func (s *Store) AdvanceOffset(ctx context.Context, offset int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE runtime_state SET telegram_offset=GREATEST(telegram_offset,$1), updated_at=now() WHERE singleton`, offset)
	return err
}

type HealthStatus struct {
	Received      int64 `json:"received"`
	StuckReceived int64 `json:"stuck_received"`
	Failed        int64 `json:"failed"`
}

func (s *Store) HealthStatus(ctx context.Context, staleAfter time.Duration) (HealthStatus, error) {
	var h HealthStatus
	err := s.pool.QueryRow(ctx, `
			SELECT COUNT(*) FILTER (WHERE status='received'),
			       COUNT(*) FILTER (WHERE status='received' AND created_at < now() - make_interval(secs => $1)),
			       COUNT(*) FILTER (WHERE status='failed')
			FROM jobs`, staleAfter.Seconds()).Scan(&h.Received, &h.StuckReceived, &h.Failed)
	return h, err
}

// ReapStaleJobs fails jobs left in received after a crash or a killed process.
// Without it a single interrupted transcription keeps jobs_stuck_received above
// zero forever, which pins /healthz at 503 and the container at unhealthy: no
// other path ever moves a received row to a terminal state.
func (s *Store) ReapStaleJobs(ctx context.Context, staleAfter time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE jobs SET status='failed', error_code='stale', finished_at=now()
		WHERE status='received' AND created_at < now() - make_interval(secs => $1)`, staleAfter.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) Cleanup(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().Add(-retention)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	jobs, err := tx.Exec(ctx, `DELETE FROM jobs WHERE status IN ('sent','empty','failed') AND finished_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	transcripts, err := tx.Exec(ctx, `DELETE FROM transcripts WHERE last_used_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return jobs.RowsAffected() + transcripts.RowsAffected(), nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
