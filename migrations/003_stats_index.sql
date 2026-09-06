-- SPDX-License-Identifier: Apache-2.0
-- The statistics panel buckets finished jobs by hour. Without an index that
-- histogram is a full scan of jobs on every view and every tab switch.

CREATE INDEX IF NOT EXISTS ix_jobs_sent_finished
  ON jobs (finished_at)
  WHERE status = 'sent';

CREATE INDEX IF NOT EXISTS ix_jobs_sent_user_finished
  ON jobs (telegram_user_id, finished_at)
  WHERE status = 'sent';
