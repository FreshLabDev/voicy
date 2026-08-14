<h1 align="center">VoiceToText</h1>

<p align="center"><strong>Send a voice or a video circle. Get the text.</strong><br/>A Go Telegram bot on Deepgram Listen, with a first-party Bot API client.</p>

<p align="center">
  <a href="docs/versioning.md"><img src="https://img.shields.io/badge/version-v0.1.0-dev-4c8c4a?style=for-the-badge&labelColor=0f172a" alt="version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-334155?style=for-the-badge&labelColor=0f172a" alt="license"></a>
</p>

---

## What it does

In a private chat, send a voice message or a video circle. The bot streams the audio through [Deepgram](https://developers.deepgram.com/docs/pre-recorded-audio) and replies with the transcript. Partial text appears as a Telegram draft while Deepgram is still listening; the final message is a normal (or ephemeral) send.

In a group the bot stays quiet unless you ask:

| Command | Result |
|:--|:--|
| `/v` as a reply to a voice or circle | Transcript visible to the chat |
| `/vp` as a reply to a voice or circle | Transcript visible only to you |

The same Telegram `file_id` is transcribed once. We store the id and the text, never the audio bytes.

## Quick start

You need Docker. Copy `.env.example` to `.env` and set `TELEGRAM_BOT_TOKEN` and `DEEPGRAM_API_KEY`.

```sh
docker compose up --build
```

`GET /healthz` on port 8080 reports database and Telegram poll freshness.

## Layout

```text
cmd/voicetotext/       process wiring
internal/telegram/     first-party Bot API HTTP client
internal/deepgram/     streaming WS + prererecorded POST
internal/decide/       update → action (no I/O)
internal/bot/          handlers
internal/db/           voicetotext schema
migrations/
deploy/core-init.sql   local-only core.touch seed
```

See [razvitie.md](razvitie.md) for the product contract and slice order.

## License

Apache-2.0. See `LICENSE` and `NOTICE`.
