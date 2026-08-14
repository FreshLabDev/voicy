# Changelog

## Unreleased

### Added

- First cut of the Go voice-to-text bot: DM voice/circle transcription, group `/v` and `/vp`, Deepgram streaming with prererecorded fallback, Telegram `sendMessageDraft`, and `file_id` cache.
- Group `/vp` is an ephemeral command: placeholder inside the 15s window, then `editEphemeralMessageText` (DM fallback).
