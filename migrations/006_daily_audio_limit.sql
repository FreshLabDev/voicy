-- SPDX-License-Identifier: Apache-2.0
-- Deepgram bills every second of audio it hears, and nothing stopped one
-- person from spending any amount of them. Each job now records the seconds
-- Deepgram was paid for, so a person can be held to a rolling daily allowance.
-- A job refused for that reason ends as 'limited': it is neither a delivery nor
-- a failure, and it must not show up in error counts.

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS audio_seconds DOUBLE PRECISION
  CHECK (audio_seconds >= 0);

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_status_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_status_check
  CHECK (status IN ('received', 'sent', 'empty', 'failed', 'limited'));
