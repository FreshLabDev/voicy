# AGENTS.md

Keep VoiceToText minimal, private by default, and production-minded.

## Product Boundaries

- STT is Deepgram only (streaming first, prererecorded fallback). No Whisper, no other provider.
- Telegram is a first-party HTTP wrapper in `internal/telegram`. Do not add a bot SDK.
- DM: any voice or video note is transcribed. Groups: only `/v` (public) or `/vp` (requester-only) as a reply to that media.
- Cache key is Telegram `file_id`. Store id + transcript, never audio bytes.
- Empty and failed runs are logged and must not increment user-facing stats. No streak.

## Data And Security

- Never log the bot token, Deepgram key, or full Telegram API URLs.
- Use `search_path=voicetotext` and call `core.touch('voicetotext', …)` before domain writes that FK `core.person`.
- Secrets live in `.env`, which is gitignored.

## Verification

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.26.5-alpine go test ./...
docker compose config
```
