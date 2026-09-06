-- SPDX-License-Identifier: Apache-2.0
-- Voicy now transcribes audio files, documents and video, not just voice
-- messages and circles. The transcript cache records the kind it came from, and
-- statistics count files as their own category.

ALTER TABLE transcripts DROP CONSTRAINT IF EXISTS transcripts_kind_check;
ALTER TABLE transcripts ADD CONSTRAINT transcripts_kind_check
  CHECK (kind IN ('voice', 'video_note', 'audio', 'document', 'video'));

ALTER TABLE user_stats ADD COLUMN IF NOT EXISTS file_count BIGINT NOT NULL DEFAULT 0
  CHECK (file_count >= 0);
