# Changelog

All notable Voicy changes are documented here.

Voicy uses SemVer-style versions with pre-release tags before `v1.0.0`. Release
notes should be copied from the relevant changelog section and lightly edited for
GitHub Releases.

## Unreleased

## v0.0.1-alpha.6 - 2026-08-22

### Operations

- Updated pinned Docker release actions to their Node 24 generations, removing
  GitHub's Node 20 deprecation annotations from the release workflow.

## v0.0.1-alpha.5 - 2026-08-22

### Fixed

- Repeated ordinary and ephemeral panel actions now treat Telegram's
  `message is not modified` response as an idempotent success instead of
  retrying the update and eventually dropping it.

## v0.0.1-alpha.4 - 2026-08-22

Voicy now has one canonical name from repository folder to production schema,
reliable Telegram initialization and job accounting, Rich Markdown transcript
delivery, complete settings, and useful personal and global statistics.

### Added

- `/start` panel with Language, Settings, Stats, Help, About, and Close.
- `/language [ru|en]` in DM opens the language panel or sets the language
  across the shared Core language hub.
- Settings for smart formatting, paragraphs, filler words, profanity filter,
  diarization, quote style, and metadata.
- Per-user Deepgram options: `smart_format`, `paragraphs`, `filler_words`,
  `profanity_filter`, `diarize_model=latest`. `detect_language` and
  `mip_opt_out` remain always-on.
- Localized speaker turns from Deepgram paragraph-level speaker metadata.
- Personal and global statistics tabs with users, transcript kinds, audio
  duration, words, Kyiv peak hour, and last detected language.
- Owner-bound deep-link recovery for a long ephemeral transcript when neither
  ephemeral Rich Markdown nor direct-message delivery is available.
- Media size and duration limits, hourly retention cleanup, and stuck-job
  health reporting.

### Changed

- Removed Deepgram WebSocket code and Telegram draft delivery. Prerecorded
  `POST /v1/listen` is the only STT transport.
- Removed transcript-document delivery and its setting. Transcripts use Bot API
  `sendRichMessage`, preserving replies and topics, with safe splitting above
  32,768 characters.
- Settings callbacks carry the desired value and are safe to retry.
- Telegram startup retries `deleteWebhook`, `getMe`, and command registration
  until all succeed. `/healthz` remains unhealthy before initialization.
- A terminal job transition and user-stat update now commit atomically and only
  once. Failed and empty jobs never affect user-facing statistics.
- `transcripts` primary key is `(file_id, variant)` and cached speaker turns are
  stored with the text.
- The folder, stack, database schema, Core bot key, role, and container are all
  named `voicy`.

### Migrations

- `002_user_settings.sql` adds settings, cache variants, speaker turns,
  retrieval tokens, and last-use timestamps.
- Core `011_voicy.sql` migrates legacy presence and language rows, renames the
  schema and role, and narrows `voicy_core` to the three shared API functions it
  uses.

### Operations

- Updated to Go 1.26.6 and fixed reachable standard-library and `x/text`
  vulnerabilities.
- CI and release workflows pin actions, pin `govulncheck`, run full release
  verification, require tagged code to be on `main`, fail on missing changelog,
  and publish an immutable image digest with SBOM and provenance.
- Production Compose accepts only an explicit `VOICY_IMAGE` reference.

## v0.0.1-alpha.3 - 2026-08-14

Menus and transcripts follow the family panel contract. Long voices keep
Deepgram paragraph breaks.

### Added

- Menu callbacks are owner-scoped (`m:<user_id>:<action>`). A foreign tap is
  ignored with a short toast.
- Close deletes the panel instead of replacing it with a checkmark.
- Empty stats show a short empty state, not zeros.
- Deepgram Listen requests `paragraphs=true`. The formatted reply uses that
  paragraph text when Deepgram returns it.

### Changed

- `/start`, Help, and Stats use `<b>title</b>` + `<i>hint</i>` +
  `<blockquote>` body, with Back/Close on subpanels.
- Transcripts are a clean blockquote. Language, duration, and confidence are
  no longer appended.

### Known Limitations

- Cached transcripts from earlier alphas stay a single paragraph until that
  `file_id` is transcribed again.

## v0.0.1-alpha.2 - 2026-08-14

File uploads now go to Deepgram prererecorded Listen instead of Live streaming.

### Changed

- `Transcribe` sends the downloaded Telegram file to `POST /v1/listen`. Default
  `Content-Type` is `audio/ogg` when Telegram does not provide one.
- Live WebSocket streaming remains in the client but is no longer the file
  upload path.

### Fixed

- Empty or mis-detected audio containers are less likely to fail Deepgram
  because files are sent as prererecorded OGG rather than a raw stream.

## v0.0.1-alpha.1 - 2026-08-14

First public alpha. A Go Telegram bot that turns a voice message or video circle
into text with Deepgram.

### Added

- DM transcription: send a voice or video circle and receive the text.
- Group `/v` replies with a public transcript of the replied voice or circle.
- Group `/vp` is an ephemeral command: a private placeholder inside Telegram's
  15-second window, then `editEphemeralMessageText`. DM fallback if the edit
  fails. Ordinary non-ephemeral group `/vp` stays quiet.
- Deepgram Live streaming (`nova-3`, `language=multi`) with prererecorded
  Listen fallback. Interim text uses Telegram `sendMessageDraft` where the
  client accepts drafts.
- `file_id` cache: store Telegram file id plus transcript, never audio bytes.
  The same file is not sent to Deepgram again.
- First-party Telegram HTTP client. No third-party Bot API SDK.
- Shared `core` identity via `core.touch`. Domain tables live in `voicy.*`.
- User-facing stats count only successful non-empty transcripts. Empty and
  failed runs are logged only.
- `/healthz`, Docker Compose for local development, and a WS04 production
  compose file that joins `core_net`.

### Operations

- Local Compose seeds a minimal `core` schema (`deploy/core-init.sql`).
- Production uses `voicy_core` on shared `core-postgres`
  (`deploy/ws04/compose.yaml`).

### Known Limitations

- Alpha: live validation is limited. Group drafts are often ignored by Telegram;
  `/v` still sends a public final message.
- Privacy mode stays on. Groups do not auto-transcribe every voice.
- Deepgram streaming of an already-downloaded file falls back to REST Listen
  when the WebSocket path fails.

### Migrations

- `001_init.sql` creates `transcripts`, `jobs`, `user_stats`, and
  `runtime_state` in the `voicy` schema.
