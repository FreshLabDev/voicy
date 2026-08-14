// SPDX-License-Identifier: Apache-2.0
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/FreshLabDev/voicy/internal/deepgram"
	"github.com/FreshLabDev/voicy/internal/stats"
	"github.com/FreshLabDev/voicy/internal/telegram"
)

type Store struct {
	pool *pgxpool.Pool
}

func Connect(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	if cfg.ConnConfig.RuntimeParams["search_path"] == "" {
		cfg.ConnConfig.RuntimeParams["search_path"] = "voicetotext,core,public"
	}
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
		"voicetotext", user.ID, nullString(user.Username), nullString(user.FirstName),
		nullString(user.LastName), nullString(user.LanguageCode), chatID, chatType, chatTitle, chatUname, user.IsBot)
	return err
}

type Cached struct {
	FileID      string
	Kind        string
	Transcript  string
	Language    string
	Confidence  float64
	Duration    float64
	RequestID   string
	WordCount   int
}

func (s *Store) GetByFileID(ctx context.Context, fileID string) (Cached, bool, error) {
	var row Cached
	err := s.pool.QueryRow(ctx, `
		SELECT file_id, kind, transcript, COALESCE(detected_language,''), COALESCE(confidence,0),
		       COALESCE(duration_seconds,0), COALESCE(deepgram_request_id,''), word_count
		FROM transcripts WHERE file_id=$1`, fileID).
		Scan(&row.FileID, &row.Kind, &row.Transcript, &row.Language, &row.Confidence, &row.Duration, &row.RequestID, &row.WordCount)
	if err == pgx.ErrNoRows {
		return Cached{}, false, nil
	}
	if err != nil {
		return Cached{}, false, err
	}
	return row, true, nil
}

func (s *Store) SaveTranscript(ctx context.Context, fileID, uniqueID, kind string, res deepgram.Result) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO transcripts (file_id, file_unique_id, kind, transcript, detected_language, confidence, duration_seconds, deepgram_request_id, word_count)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (file_id) DO UPDATE SET
		  transcript = EXCLUDED.transcript,
		  detected_language = EXCLUDED.detected_language,
		  confidence = EXCLUDED.confidence,
		  duration_seconds = EXCLUDED.duration_seconds,
		  deepgram_request_id = EXCLUDED.deepgram_request_id,
		  word_count = EXCLUDED.word_count`,
		fileID, nullString(uniqueID), kind, res.Text, nullString(res.Language), res.Confidence, res.Duration, nullString(res.RequestID), res.WordCount)
	return err
}

func (s *Store) CreateJob(ctx context.Context, updateID, messageID, userID, chatID int64, kind, fileID string) (int64, string, error) {
	var id int64
	var status string
	err := s.pool.QueryRow(ctx, `
		INSERT INTO jobs (telegram_update_id, telegram_message_id, telegram_user_id, chat_id, kind, file_id)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (telegram_update_id) DO UPDATE SET telegram_update_id = EXCLUDED.telegram_update_id
		RETURNING id, status`, updateID, messageID, userID, chatID, kind, fileID).Scan(&id, &status)
	return id, status, err
}

func (s *Store) FinishJob(ctx context.Context, id int64, status, errCode string, cacheHit bool) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET status=$2, error_code=$3, cache_hit=$4, finished_at=now() WHERE id=$1`,
		id, status, nullString(errCode), cacheHit)
	return err
}

func (s *Store) RecordSuccess(ctx context.Context, userID int64, kind string, res deepgram.Result) error {
	if !stats.ShouldCount("sent", res.Text) {
		return nil
	}
	voice := 0
	circle := 0
	if kind == "video_note" {
		circle = 1
	} else {
		voice = 1
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_stats (telegram_user_id, transcriptions, voice_count, video_note_count, duration_seconds, words, last_language, first_at, last_at)
		VALUES ($1,1,$2,$3,$4,$5,$6,now(),now())
		ON CONFLICT (telegram_user_id) DO UPDATE SET
		  transcriptions = user_stats.transcriptions + 1,
		  voice_count = user_stats.voice_count + EXCLUDED.voice_count,
		  video_note_count = user_stats.video_note_count + EXCLUDED.video_note_count,
		  duration_seconds = user_stats.duration_seconds + EXCLUDED.duration_seconds,
		  words = user_stats.words + EXCLUDED.words,
		  last_language = COALESCE(EXCLUDED.last_language, user_stats.last_language),
		  last_at = now()`,
		userID, voice, circle, res.Duration, res.WordCount, nullString(res.Language))
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
	return out, err
}

func (s *Store) GlobalStats(ctx context.Context) (stats.Snapshot, error) {
	out := stats.EmptySnapshot()
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(transcriptions),0), COALESCE(SUM(voice_count),0), COALESCE(SUM(video_note_count),0),
		       COALESCE(SUM(duration_seconds),0), COALESCE(SUM(words),0), COUNT(*)
		FROM user_stats`).
		Scan(&out.Transcriptions, &out.Voice, &out.VideoNotes, &out.DurationSec, &out.Words, &out.Users)
	return out, err
}

func (s *Store) Offset(ctx context.Context) (int64, error) {
	var off int64
	err := s.pool.QueryRow(ctx, `SELECT telegram_offset FROM runtime_state WHERE singleton`).Scan(&off)
	return off, err
}

func (s *Store) AdvanceOffset(ctx context.Context, offset int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE runtime_state SET telegram_offset=$1, updated_at=now() WHERE singleton`, offset)
	return err
}

type HealthStatus struct {
	Received int64 `json:"received"`
	Failed   int64 `json:"failed"`
}

func (s *Store) HealthStatus(ctx context.Context) (HealthStatus, error) {
	var h HealthStatus
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status='received'),
		       COUNT(*) FILTER (WHERE status='failed')
		FROM jobs`).Scan(&h.Received, &h.Failed)
	return h, err
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
