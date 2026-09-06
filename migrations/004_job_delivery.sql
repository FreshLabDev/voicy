-- SPDX-License-Identifier: Apache-2.0
-- Delivery and completion are two writes. If the process loses the database
-- between them, the update is retried and the transcript is sent twice. This
-- column records the moment a transcript reached the user, so a retry can skip
-- delivery instead of repeating it.

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS delivered_at TIMESTAMPTZ;
