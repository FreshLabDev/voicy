# Architecture

Voicy is one Go service with two runtime surfaces:

- Telegram long polling for user interaction.
- A small HTTP server for `/healthz`.

The service stores its durable state in PostgreSQL. Voicy's own tables live in
a `voicetotext` schema (transcript cache, jobs, user stats, polling offset), and
Telegram identity/presence is delegated to the shared Asterfield `core` schema
(`core.person`, `core.chat`) that Voicy upserts through the `SECURITY DEFINER`
`core.touch` function before any dependent write. In production this is the
shared `core-postgres` database; local `docker compose` seeds a minimal `core`
schema (`deploy/core-init.sql`) so migrations boot.

## Packages

- `cmd/voicy`: process wiring and graceful shutdown.
- `internal/config`: environment parsing and required setting checks.
- `internal/db`: PostgreSQL store, cache, stats, and startup migrations.
- `internal/telegram`: first-party Telegram Bot API HTTP client.
- `internal/deepgram`: Live WebSocket streaming plus prererecorded POST.
- `internal/decide`: update → action, no I/O.
- `internal/transcript`: HTML formatting and UI copy.
- `internal/bot`: handlers, cache lookup, delivery, ephemeral `/vp`.
- `internal/stats`: which finished jobs count toward user-facing aggregates.
- `internal/health`: `/healthz` payload.

## Data Flow

1. Telegram delivers an update over long poll.
2. `decide` classifies it: ignore, start, nudge, or transcribe.
3. `core.touch` records identity and presence.
4. A `file_id` cache hit returns the stored transcript without Deepgram.
5. On a miss, Voicy downloads the file from Telegram, streams bytes to Deepgram
   Live, and falls back to prererecorded Listen if the WebSocket fails.
6. Interim text is sent with `sendMessageDraft` when the chat accepts drafts.
7. Successful non-empty text is stored under `file_id` and sent as the final
   message. Empty and failed runs are logged and do not increment `user_stats`.

Group `/vp` is registered as an ephemeral command. The bot first replies to the
incoming `ephemeral_message_id` (15-second window), then edits that placeholder
with `editEphemeralMessageText` after STT. A late `sendMessage` with only
`receiver_user_id` is not used for non-admin group bots.

## Decisions

- Long polling keeps local development simple and avoids exposing Telegram
  webhook routes.
- One service keeps the codebase small. There is no separate queue for this
  alpha: Deepgram is called in the update handler.
- Voicy owns the `voicetotext` schema and, in production, connects to the
  shared `core-postgres` as `voicetotext_core` with `search_path=voicetotext`.
- Telegram is a first-party HTTP client. A third-party bot SDK is out of scope.
- Deepgram Live is the preferred path because the product wants streaming
  drafts. Prerecorded Listen remains the reliable fallback for container
  formats Telegram already produced (OGG/Opus, MP4).
- Cache key is Telegram `file_id`. Audio bytes are not stored.
- Privacy mode stays on. Groups stay quiet unless `/v` or `/vp` is explicit.
