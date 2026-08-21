# Voicy: current contract

Voicy is a minimal private-by-default Telegram voice-to-text bot.

- One Go service, long polling only.
- Deepgram prerecorded Listen only, no streaming.
- DM voice and video circles transcribe automatically.
- Groups require `/v` for public output or ephemeral `/vp` for requester-only
  output.
- Long output uses Rich Markdown and message splitting, never files.
- Cache is `(file_id, settings variant)` and never stores audio bytes.
- Domain data belongs to `voicy`; identity and language belong to shared Core.
- Only successful non-empty jobs increment personal and global statistics.
- Health requires PostgreSQL, Telegram initialization, fresh polling, and no
  stuck received jobs.

The implementation and operational details live in `docs/architecture.md`,
`docs/telegram.md`, and `docs/releases.md`.
