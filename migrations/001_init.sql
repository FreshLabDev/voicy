-- SPDX-License-Identifier: Apache-2.0
-- Domain tables in search_path=voicy. Identity stays in core.*.

CREATE TABLE IF NOT EXISTS transcripts (
  file_id TEXT PRIMARY KEY,
  file_unique_id TEXT,
  kind TEXT NOT NULL CHECK (kind IN ('voice', 'video_note')),
  transcript TEXT NOT NULL,
  detected_language TEXT,
  confidence DOUBLE PRECISION,
  duration_seconds DOUBLE PRECISION,
  deepgram_request_id TEXT,
  word_count INTEGER NOT NULL DEFAULT 0 CHECK (word_count >= 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS jobs (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  telegram_update_id BIGINT NOT NULL UNIQUE,
  telegram_message_id BIGINT NOT NULL,
  telegram_user_id BIGINT NOT NULL REFERENCES core.person(telegram_user_id) ON DELETE CASCADE,
  chat_id BIGINT NOT NULL,
  kind TEXT,
  file_id TEXT,
  status TEXT NOT NULL DEFAULT 'received' CHECK (status IN ('received', 'sent', 'empty', 'failed')),
  error_code TEXT,
  cache_hit BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS ix_jobs_user_created ON jobs (telegram_user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS ix_jobs_status_created ON jobs (status, created_at DESC);

CREATE TABLE IF NOT EXISTS user_stats (
  telegram_user_id BIGINT PRIMARY KEY REFERENCES core.person(telegram_user_id) ON DELETE CASCADE,
  transcriptions BIGINT NOT NULL DEFAULT 0 CHECK (transcriptions >= 0),
  voice_count BIGINT NOT NULL DEFAULT 0 CHECK (voice_count >= 0),
  video_note_count BIGINT NOT NULL DEFAULT 0 CHECK (video_note_count >= 0),
  duration_seconds DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (duration_seconds >= 0),
  words BIGINT NOT NULL DEFAULT 0 CHECK (words >= 0),
  last_language TEXT,
  first_at TIMESTAMPTZ,
  last_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS runtime_state (
  singleton BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
  telegram_offset BIGINT NOT NULL DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO runtime_state(singleton) VALUES (true) ON CONFLICT DO NOTHING;
