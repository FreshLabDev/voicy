# AGENTS.md

Keep Voicy minimal, private by default, and production-minded.

## Project Shape

- Voicy is one Go service (module `github.com/FreshLabDev/voicy`).
- PostgreSQL is the only durable store. Domain tables live in a `voicy`
  schema. Telegram identity and presence are delegated to shared `core`
  (`core.touch('voicy', …)`).
- Telegram goes through `github.com/FreshLabDev/tg`, the client shared by the
  bot family. Voicy keeps no private HTTP client: transport, retries, token
  redaction, rich and ephemeral messages, and local Bot API files all live
  there. Long polling only.
- `Preflight` runs before the bot starts and names the methods Voicy cannot
  work without. A server that lacks them is a startup failure, never a bot
  that polls and answers nothing.

## Product Boundaries

- STT is Deepgram only. File uploads use prerecorded Listen. No Whisper.
- DM: voice, video notes and audio files are transcribed. Video and documents
  need an explicit `/v` so no Deepgram call is spent on a file shared for some
  other reason.
- User-facing text lives in `internal/i18n/translations.json`, never inline in
  Go. Every key must exist in every offered language.
- Groups: only `/v` (public) or `/vp` (ephemeral, requester-only) as a reply
  to that media. Bare group voices stay quiet.
- Cache key is Telegram `file_id`. Store id + transcript, never audio bytes.
- Empty and failed runs are logged and must not increment user-facing stats.

## Data And Security

- Never log the bot token, Deepgram key, or full Telegram API URLs.
- Connect with `search_path=voicy` and call `core.touch` before domain
  writes that FK `core.person`.
- Secrets live in `.env`, which is gitignored.

## Versioning

- Develop on `dev`. Publish releases from `main`.
- Follow `docs/versioning.md`. The first line starts at `v0.0.1-alpha.1`.
- Use patch versions for fixes, minor versions for MVP-compatible product or
  operations improvements, and reserve `v1.0.0` for a stable production contract.
- Treat required env vars, the `voicy` schema, Deepgram/Telegram
  contracts, group `/v` `/vp` semantics, and the `file_id` cache as
  breaking-sensitive before `v1.0.0`.

## Changelog And Releases

- Keep notable changes under `## Unreleased` in `CHANGELOG.md`.
- Follow `docs/releases.md` for changelog sections and GitHub Release commands.
- Mark `alpha`, `beta`, and `rc` GitHub Releases as pre-releases.
- Do not publish a GitHub Release until verification results and release notes
  match the tagged code.

## Verification

```sh
go test ./...
go vet ./...
docker compose config
```

The database tests need a real PostgreSQL and a disposable database whose name
contains `test`; they refuse to run anywhere else:

```sh
VOICY_TEST_DATABASE_URL=postgres://voicy:voicy@localhost:5432/voicy_test?sslmode=disable \
  go test -tags=integration ./internal/db/
```

CI also runs `go mod verify`, `go test -race ./...`, Docker build, Compose
validation, and `govulncheck`.

## Release Checklist

- `/start` opens the family panel in a private chat and an ephemeral prompt in a group.
- Close deletes the panel. Menu callbacks are owner-scoped.
- A DM voice or video circle returns a transcript.
- Group `/v` on a reply is public. Group `/vp` is ephemeral then edited.
- A repeated `file_id` is served from cache.
- Empty and failed jobs do not increment `user_stats`.
- `/healthz` reports database and Telegram polling freshness.
- `/metrics` renders without error and names every counter `voicy_*_total`.
- A retried update never sends the same transcript twice.

## License

Voicy is licensed under Apache-2.0. Preserve `LICENSE` and `NOTICE`.
