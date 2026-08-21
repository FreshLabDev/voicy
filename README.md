<h1 align="center">Voicy</h1>

<p align="center"><strong>Send a voice or a video circle. Get the text.</strong><br/>A small Go Telegram bot that transcribes with Deepgram.</p>

<p align="center">
  <a href="https://github.com/FreshLabDev/voicy/releases"><img src="https://img.shields.io/github/v/release/FreshLabDev/voicy?include_prereleases&sort=semver&style=for-the-badge&label=latest&labelColor=0f172a&color=4c8c4a" alt="latest version"></a>
  <a href="docs/versioning.md"><img src="https://img.shields.io/badge/version-v0.0.1--alpha.4-4c8c4a?style=for-the-badge&labelColor=0f172a" alt="current version"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/FreshLabDev/voicy?style=for-the-badge&logo=go&logoColor=white&label=go&labelColor=0f172a&color=00ADD8" alt="go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-334155?style=for-the-badge&labelColor=0f172a" alt="license"></a>
  <a href="https://t.me/voicyin_bot"><img src="https://img.shields.io/badge/telegram-%40voicyin__bot-26A5E4?style=for-the-badge&logo=telegram&logoColor=white&labelColor=0f172a" alt="telegram bot"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> ·
  <a href="#using-voicy">Using Voicy</a> ·
  <a href="#how-it-works">How It Works</a> ·
  <a href="#configuration">Configuration</a> ·
  <a href="#security">Security</a> ·
  <a href="#docs">Docs</a>
</p>

---

## The Problem

Voice notes and video circles are fast to send and slow to read later. Pasting
them into a separate transcriber breaks the chat, and a bot that talks on every
group voice becomes noise.

Voicy keeps the first alpha deliberately narrow:

| Need | Voicy approach |
|:--|:--|
| One action | Send a voice or circle, get the text back |
| Quiet groups | Transcribes only on `/v` or `/vp` as a reply |
| Private ask | `/vp` is ephemeral and visible only to the requester |
| Cheap repeats | Caches by Telegram `file_id`; audio stays on Telegram |
| Familiar stack | Go, first-party Bot API HTTP, shared Asterfield `core` |

> **Result:** one small service that turns speech already in Telegram into
> readable text without becoming a group chatterbot.

---

## Status

| Channel | Version | Meaning |
|:--|:--|:--|
| Latest | `v0.0.1-alpha.4` | Alpha: Rich Markdown, reliable jobs, settings, and human stats |
| Stable | — | Not yet. This line is pre-release until `v0.0.1` |

The bot is live for limited testing as [@voicyin_bot](https://t.me/voicyin_bot).
Alpha means the Deepgram and Telegram paths work, but the public contract is
still allowed to change.

---

## Quick Start

You need Docker, a Telegram bot token from
[BotFather](https://t.me/BotFather), and a Deepgram API key.

```sh
# 1. Copy local configuration
cp .env.example .env

# 2. Fill TELEGRAM_BOT_TOKEN and DEEPGRAM_API_KEY
$EDITOR .env

# 3. Start Voicy and PostgreSQL
docker compose up --build
```

Voicy runs startup migrations from `migrations/` and records completed versions
in `schema_migrations`. Keep `AUTO_MIGRATE=true` for local development.

The bundled `docker compose` is for local development only. It runs a local
PostgreSQL and seeds a minimal shared `core` schema (`deploy/core-init.sql`) so
migrations that reference `core.person` / `core.chat` boot cleanly. In the
shared production deployment Voicy instead connects to the existing
`core-postgres`; use the production notes in
[`docs/releases.md`](docs/releases.md).

---

## Using Voicy

1. Open [@voicyin_bot](https://t.me/voicyin_bot) and send `/start`.
2. In a private chat, send a voice message or a video circle.
3. Wait for the transcript.
4. In a group, reply to a voice or circle with `/v` (everyone) or `/vp`
   (only you).

`/start` in a group is an ephemeral Bot API 10.2 command. Its prompt is visible
only to the user who invoked it. Bare voices in a group are ignored.

A file that Voicy has already transcribed is served from cache. The audio file
stays on Telegram; Voicy stores the `file_id` and the text. Long transcripts
use Bot API Rich Markdown messages up to 32,768 characters each and split into
multiple readable messages when needed. Voicy never sends transcript files.

---

## How It Works

Voicy is one Go service with PostgreSQL as its only durable store. Domain tables
— transcript cache, jobs, user stats, poll offset — live in a `voicy`
schema. Telegram identity and presence are delegated to a shared `core` schema
(`core.person`, `core.chat`), which Voicy upserts via `core.touch` before any
dependent write. In production that schema lives in the shared `core-postgres`
database; local `docker compose` seeds a minimal `core` schema so development
boots the same way.

```text
telegram poller  -> decide -> cache or Deepgram
http server      -> /healthz
```

Transcription path:

```text
getFile -> bounded download
  -> Deepgram prerecorded POST /v1/listen
  -> persist file_id + text
  -> send Rich Markdown
```

---

## MVP Scope

| Included | Excluded |
|:--|:--|
| Voice notes and video circles | Arbitrary audio/video documents |
| Deepgram `nova-3` prerecorded Listen | Whisper or another STT |
| DM implicit transcribe; group `/v` and `/vp` | Auto-transcribe every group voice |
| `file_id` cache | Stored audio bytes |
| `/healthz` | Public metrics surface in this alpha |

---

## Configuration

| Variable | Required | Default | Description |
|:--|:--:|:--|:--|
| `TELEGRAM_BOT_TOKEN` | yes | — | Bot token from BotFather |
| `DEEPGRAM_API_KEY` | yes | — | Deepgram project API key |
| `DATABASE_URL` | yes | — | PostgreSQL URL for `voicy_core`; the service enforces `search_path=voicy` |
| `HTTP_ADDR` | no | `:8080` | HTTP listen address |
| `MIGRATIONS_DIR` | no | `./migrations` | Migration directory |
| `AUTO_MIGRATE` | no | `true` | Run migrations on startup |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, or `error` |
| `TRANSCRIPT_RETENTION` | no | `2160h` | Retain terminal jobs and cached transcripts since last use |
| `MAX_MEDIA_BYTES` | no | `20971520` | Maximum Telegram media download size |
| `MAX_MEDIA_DURATION` | no | `1h` | Maximum voice or video-circle duration |

---

## Security

- Telegram is called over HTTPS with a first-party client. Transport errors
  redact the bot token.
- Deepgram requests use `Authorization: Token …` and `mip_opt_out=true`.
  Telegram file URLs are never sent to Deepgram.
- Empty and failed transcriptions are logged with `file_id` and `update_id`,
  not audio bytes or full transcripts.
- Logs avoid bot tokens, Deepgram keys, and full Bot API URLs.

---

## Deployment

Local Compose is not the production source. Production runs from
`/opt/stacks/voicy` on the shared `core_net` and the `voicy_core`
role. See [`docs/releases.md`](docs/releases.md).

`/healthz` reports database status, Telegram initialization, polling freshness,
stuck jobs, job counts, and build metadata without exposing secrets.

---

## Testing

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.26.6-alpine go test ./...
docker run --rm -v "$PWD":/src -w /src golang:1.26.6-alpine go vet ./...
cp .env.example .env
docker compose config
```

---

## Docs

| Document | Purpose |
|:--|:--|
| [Architecture](docs/architecture.md) | Service structure and core decisions |
| [Telegram behavior](docs/telegram.md) | Commands, privacy, Rich Markdown, cache |
| [Versioning](docs/versioning.md) | Pre-release and stable version line |
| [Release process](docs/releases.md) | Changelog and GitHub Release rules |

---

<p align="center">
  <a href="https://github.com/FreshLabDev/voicy/releases">Releases</a> ·
  <a href="CHANGELOG.md">Changelog</a> ·
  <a href="LICENSE">Apache-2.0</a> ·
  <a href="NOTICE">NOTICE</a>
</p>

<p align="center">
  Voicy is open source software by Asterfield.<br/>
  Copyright 2026 Asterfield.
</p>
