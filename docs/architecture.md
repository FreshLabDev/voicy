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

1. Long polling returns a batch of updates.
2. The batch is fanned out across `MAX_CONCURRENT_JOBS` workers. Updates from
   one person run in arrival order on a single worker, so two voices cannot
   race for the same job row or arrive reordered. The batch is a barrier: the
   poll offset advances only after all of it is finished.
3. `internal/decide` classifies each update without I/O.
3. `core.touch` records identity and presence before domain writes.
4. A job row is created idempotently for the Telegram `update_id`.
5. A `(file_id, variant)` cache hit skips Telegram download and Deepgram.
6. On a miss, Voicy enforces duration and size limits, downloads bounded bytes,
   and calls Deepgram `POST /v1/listen`.
7. The transcript and optional speaker turns are cached.
8. Delivery uses `sendRichMessage` with an HTML `rich_message`. Each complete
   part is at most 32,768 characters and preserves replies and topics.
9. The terminal job transition and user-stat increment commit together.

Empty and failed jobs are retained for operations but never increment user
statistics. Terminal jobs and transcripts unused past `TRANSCRIPT_RETENTION`
are deleted by the hourly cleanup loop.

## Reliability

- The first PostgreSQL connection is retried with backoff, so container DNS
  that is not ready at start does not become a restart loop.
- Telegram initialization retries until `deleteWebhook`, `getMe`, and both
  command scopes succeed.
- An update is retried three times before its received job is failed and its
  offset advances. Settings callbacks carry the desired value, so replay is
  idempotent.
- Poll offsets only move forward. A failed offset or job write is logged and
  the loop continues: `update_id` keys every job, so a replay is deduplicated.
- A job left in `received` past `JOB_STALE_AFTER` is failed by the reaper. The
  same threshold drives the stuck-job field in `/healthz`, so an interrupted
  transcription cannot hold the service unhealthy forever.
- Deepgram and Telegram bodies have explicit size limits and timeouts. A
  Deepgram request is retried on 408, 429, and 5xx, and its deadline scales
  with the length of the audio.
- Statistics are served from an in-memory snapshot cache, so the peak-hour
  histogram cannot be triggered once per tab tap.
- `/healthz` is unhealthy until Telegram initialization succeeds, polling is
  fresh, PostgreSQL responds, and no received job is stuck for 15 minutes.

## Privacy

DM media is implicit. Groups remain quiet unless `/v` or `/vp` replies to a
voice or video circle. `/vp` opens an ephemeral placeholder inside Telegram's
window. Short results edit that message. A longer result uses ephemeral Rich
Markdown when possible, then DM, then an owner-bound deep link as recovery.

## Packages

- `cmd/voicy`: wiring, cleanup and reaper loops, HTTP server, shutdown.
- `internal/httpx`: the tuned HTTP transport shared by Telegram and Deepgram.
- `internal/bot`: handlers, retries, cache and delivery orchestration.
- `internal/config`: validated environment configuration.
- `internal/db`: migrations, jobs, cache, settings, statistics.
- `internal/decide`: update classification.
- `internal/deepgram`: prerecorded Listen client and response extraction.
- `internal/health`: readiness and liveness contract.
- `internal/settings`: settings registry and cache variants.
- `internal/telegram`: Bot API HTTP transport.
- `internal/transcript`: localized panels, rich HTML, splitting, stats.
