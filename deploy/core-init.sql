-- SPDX-License-Identifier: Apache-2.0
-- LOCAL-DEV ONLY. Minimal core.person/core.chat/core.touch plus the voicy schema.
CREATE SCHEMA IF NOT EXISTS core;
CREATE SCHEMA IF NOT EXISTS voicy;

CREATE TABLE IF NOT EXISTS core.person (
  telegram_user_id bigint PRIMARY KEY,
  username text, first_name text, last_name text,
  is_bot boolean NOT NULL DEFAULT false,
  tg_language_code text,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at  timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS core.chat (
  chat_id bigint PRIMARY KEY,
  type text, title text, username text,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at  timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION core.touch(
  p_bot text, p_user_id bigint,
  p_username text DEFAULT NULL, p_first_name text DEFAULT NULL, p_last_name text DEFAULT NULL,
  p_tg_lang text DEFAULT NULL, p_chat_id bigint DEFAULT NULL, p_chat_type text DEFAULT NULL,
  p_chat_title text DEFAULT NULL, p_chat_uname text DEFAULT NULL, p_is_bot boolean DEFAULT false,
  p_at timestamptz DEFAULT now()
) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO core.person AS pe (telegram_user_id, username, first_name, last_name, is_bot, tg_language_code, last_seen_at, updated_at)
  VALUES (p_user_id, p_username, p_first_name, p_last_name, p_is_bot, p_tg_lang, p_at, p_at)
  ON CONFLICT (telegram_user_id) DO UPDATE SET
    username = COALESCE(EXCLUDED.username, pe.username),
    first_name = COALESCE(EXCLUDED.first_name, pe.first_name),
    last_name = COALESCE(EXCLUDED.last_name, pe.last_name),
    is_bot = EXCLUDED.is_bot,
    tg_language_code = COALESCE(EXCLUDED.tg_language_code, pe.tg_language_code),
    last_seen_at = EXCLUDED.last_seen_at, updated_at = EXCLUDED.updated_at;
  IF p_chat_id IS NOT NULL AND p_chat_id <> 0 THEN
    INSERT INTO core.chat AS ch (chat_id, type, title, username, last_seen_at, updated_at)
    VALUES (p_chat_id, p_chat_type, p_chat_title, p_chat_uname, p_at, p_at)
    ON CONFLICT (chat_id) DO UPDATE SET
      type = COALESCE(EXCLUDED.type, ch.type),
      title = COALESCE(EXCLUDED.title, ch.title),
      username = COALESCE(EXCLUDED.username, ch.username),
      last_seen_at = EXCLUDED.last_seen_at, updated_at = EXCLUDED.updated_at;
  END IF;
  IF p_tg_lang IS NOT NULL AND btrim(p_tg_lang) <> '' THEN
    INSERT INTO core.user_language AS ul (bot, subject_id, language, source)
    VALUES (p_bot, p_user_id, lower(split_part(btrim(p_tg_lang), '-', 1)), 'client')
    ON CONFLICT (bot, subject_id) DO UPDATE SET
      language = CASE WHEN ul.source = 'manual' THEN ul.language ELSE EXCLUDED.language END,
      source   = CASE WHEN ul.source = 'manual' THEN 'manual' ELSE 'client' END,
      updated_at = now();
  END IF;
END $$;

-- Local stand-in for the core language hub. The real one keeps per-bot
-- observations with manual/auto/client ranking across the whole bot fleet;
-- one row per (bot, subject) with manual-beats-client merge is enough here.
CREATE TABLE IF NOT EXISTS core.user_language (
  bot text NOT NULL,
  subject_id bigint NOT NULL,
  language text NOT NULL,
  source text NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (bot, subject_id)
);

CREATE OR REPLACE FUNCTION core.set_language(
  p_bot text, p_scope text, p_subject bigint, p_lang text, p_source text
) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  IF p_lang IS NULL OR btrim(p_lang) = '' THEN
    RETURN;
  END IF;
  INSERT INTO core.user_language AS ul (bot, subject_id, language, source, updated_at)
  VALUES (p_bot, p_subject, lower(split_part(btrim(p_lang), '-', 1)), p_source, now())
  ON CONFLICT (bot, subject_id) DO UPDATE SET
    language = EXCLUDED.language,
    source = EXCLUDED.source,
    updated_at = now();
END $$;

CREATE OR REPLACE FUNCTION core.effective_language(
  p_user bigint, p_chat bigint DEFAULT NULL, p_prefer text DEFAULT 'user'
) RETURNS text LANGUAGE sql STABLE AS $$
  SELECT l.language
  FROM core.user_language l
  WHERE l.subject_id = p_user
  ORDER BY (l.source = 'manual') DESC, l.updated_at DESC
  LIMIT 1;
$$;
