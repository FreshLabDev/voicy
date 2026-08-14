# Changelog

All notable Voicy changes are documented here.

Voicy uses SemVer-style versions with pre-release tags before `v1.0.0`. Release
notes should be copied from the relevant changelog section and lightly edited for
GitHub Releases.

## Unreleased

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
- Shared `core` identity via `core.touch`. Domain tables live in
  `voicetotext.*`.
- User-facing stats count only successful non-empty transcripts. Empty and
  failed runs are logged only.
- `/healthz`, Docker Compose for local development, and a WS04 production
  compose file that joins `core_net`.

### Operations

- Local Compose seeds a minimal `core` schema (`deploy/core-init.sql`).
- Production uses `voicetotext_core` on shared `core-postgres`
  (`deploy/ws04/compose.yaml`). Core registration is
  `core/migrations/009_voicetotext.sql`.

### Known Limitations

- Alpha: live validation is limited. Group drafts are often ignored by Telegram;
  `/v` still sends a public final message.
- Privacy mode stays on. Groups do not auto-transcribe every voice.
- Deepgram streaming of an already-downloaded file falls back to REST Listen
  when the WebSocket path fails.

### Migrations

- `001_init.sql` creates `transcripts`, `jobs`, `user_stats`, and
  `runtime_state` in the `voicetotext` schema.
