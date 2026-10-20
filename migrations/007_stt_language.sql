-- SPDX-License-Identifier: Apache-2.0
-- The language a person asked Deepgram to listen for. Deepgram's automatic
-- detection does not know Arabic, Persian, Belarusian and about two dozen other
-- languages nova-3 does transcribe, so those only come out right when the
-- request names the language. Empty means Auto: the language is taken from the
-- person's profile in core, and nothing is named for languages detection knows.
-- Existing rows keep Auto, so no cached transcript changes meaning.

ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS stt_language TEXT NOT NULL DEFAULT '';
