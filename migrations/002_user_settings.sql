-- SPDX-License-Identifier: Apache-2.0
-- Per-user transcription and delivery settings. Identity and language stay in core.*
-- Column names must match settings.Specs keys exactly (see internal/settings).

CREATE TABLE IF NOT EXISTS user_settings (
  telegram_user_id BIGINT PRIMARY KEY REFERENCES core.person(telegram_user_id) ON DELETE CASCADE,
  smart_format BOOLEAN NOT NULL DEFAULT true,
  paragraphs BOOLEAN NOT NULL DEFAULT true,
  filler_words BOOLEAN NOT NULL DEFAULT false,
  profanity_filter BOOLEAN NOT NULL DEFAULT false,
  diarize BOOLEAN NOT NULL DEFAULT false,
  quote BOOLEAN NOT NULL DEFAULT true,
  meta BOOLEAN NOT NULL DEFAULT false,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One file can hold several transcripts, one per Deepgram option set.
-- The default matches settings.DefaultVariant for rows written before variants.
ALTER TABLE transcripts ADD COLUMN IF NOT EXISTS variant TEXT NOT NULL DEFAULT 'sf1-p1-fw0-pf0-d0';
ALTER TABLE transcripts ADD COLUMN IF NOT EXISTS speaker_turns JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE transcripts ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE transcripts DROP CONSTRAINT IF EXISTS transcripts_pkey;
ALTER TABLE transcripts ADD PRIMARY KEY (file_id, variant);

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS variant TEXT NOT NULL DEFAULT 'sf1-p1-fw0-pf0-d0';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS retrieval_token TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS ux_jobs_retrieval_token
  ON jobs (retrieval_token) WHERE retrieval_token IS NOT NULL;
