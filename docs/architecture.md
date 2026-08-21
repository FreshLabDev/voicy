# Architecture

Voicy is one Go service with Telegram long polling and a small `/healthz` HTTP
surface. PostgreSQL is its only durable store.

## Boundaries

- Domain tables live in the isolated `voicy` schema.
- Shared Telegram identity, presence, and language live in `core`.
- Voicy connects as `voicy_core`, can reference `core.person` and `core.chat`,
  and can execute only `core.touch`, `core.set_language`, and
  `core.effective_language`.
- Audio is held in memory only for the Deepgram request. It is never logged or
  stored in PostgreSQL.
- Telegram is accessed through the first-party HTTP client in
  `internal/telegram`. There is no SDK and no webhook mode.
- Speech recognition is Deepgram prerecorded Listen only. There is no streaming
  transport or draft-message path.

## Data Flow

1. Long polling returns an update.
2. `internal/decide` classifies it without I/O.
3. `core.touch` records identity and presence before domain writes.
4. A job row is created idempotently for the Telegram `update_id`.
5. A `(file_id, variant)` cache hit skips Telegram download and Deepgram.
6. On a miss, Voicy enforces duration and size limits, downloads bounded bytes,
   and calls Deepgram `POST /v1/listen`.
7. The transcript and optional speaker turns are cached.
8. Delivery uses `sendRichMessage`. Each complete Rich Markdown part is at
   most 32,768 characters and preserves replies and topics.
9. The terminal job transition and user-stat increment commit together.

Empty and failed jobs are retained for operations but never increment user
statistics. Terminal jobs and transcripts unused past `TRANSCRIPT_RETENTION`
are deleted by the hourly cleanup loop.

## Reliability

- Telegram initialization retries until `deleteWebhook`, `getMe`, and both
  command scopes succeed.
- An update is retried three times before its received job is failed and its
  offset advances. Settings callbacks carry the desired value, so replay is
  idempotent.
- Poll offsets only move forward.
- Deepgram and Telegram bodies have explicit size limits and timeouts.
- `/healthz` is unhealthy until Telegram initialization succeeds, polling is
  fresh, PostgreSQL responds, and no received job is stuck for 15 minutes.

## Privacy

DM media is implicit. Groups remain quiet unless `/v` or `/vp` replies to a
voice or video circle. `/vp` opens an ephemeral placeholder inside Telegram's
window. Short results edit that message. A longer result uses ephemeral Rich
Markdown when possible, then DM, then an owner-bound deep link as recovery.

## Packages

- `cmd/voicy`: wiring, cleanup loop, HTTP server, shutdown.
- `internal/bot`: handlers, retries, cache and delivery orchestration.
- `internal/config`: validated environment configuration.
- `internal/db`: migrations, jobs, cache, settings, statistics.
- `internal/decide`: update classification.
- `internal/deepgram`: prerecorded Listen client and response extraction.
- `internal/health`: readiness and liveness contract.
- `internal/settings`: settings registry and cache variants.
- `internal/telegram`: Bot API HTTP transport.
- `internal/transcript`: localized panels, Rich Markdown, splitting, stats.
