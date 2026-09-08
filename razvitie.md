# Voicy: current contract

Voicy is a minimal private-by-default Telegram voice-to-text bot.

- One Go service, long polling only.
- Telegram runs on the shared `github.com/FreshLabDev/tg` client, and the bot
  refuses to start against a server that lacks the methods it needs.
- Deepgram prerecorded Listen only, no streaming.
- DM voice, circles and audio files transcribe automatically; video and
  documents need an explicit `/v`.
- Media is streamed to disk, and video is reduced to an audio track first.
- Groups require `/v` for public output or ephemeral `/vp` for requester-only
  output.
- Long output uses rich HTML messages and splitting, never files.
- Cache is `(file_id, settings variant)` and never stores audio bytes.
- Domain data belongs to `voicy`; identity and language belong to shared Core.
- Only successful non-empty jobs increment personal and global statistics.
- Health requires PostgreSQL, Telegram initialization, fresh polling, and no
  stuck received jobs.
- A transcript is delivered at most once per Telegram update.
- Telegram may be reached through a self-hosted Bot API server.

The implementation and operational details live in `docs/architecture.md`,
`docs/telegram.md`, and `docs/releases.md`.
