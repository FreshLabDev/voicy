# Telegram

## Commands

| Command | Where | Effect |
|:--|:--|:--|
| `/start` | DM | Inline menu |
| `/start` | groups | Ephemeral DM prompt (Bot API 10.2) |
| `/v` | groups (and DM) | Transcribe the replied voice/circle for everyone |
| `/vp` | groups (ephemeral) and DM | Transcribe only for the requester |
| `/stats` `/help` | DM (also from the menu) | Stats / help panels |

A command is handled only when it is bare or addressed to this bot. Unknown commands nudge in private chats only.

Group `/vp` is registered `is_ephemeral`. The bot immediately replies to the incoming `ephemeral_message_id` with a placeholder (15-second window), then calls `editEphemeralMessageText` with the transcript. A late `sendMessage` with only `receiver_user_id` is not used — that works for chat-admin bots, not a normal group member bot. If the edit fails, the result is sent in DM.

In a group, a voice or circle without `/v` or `/vp` is ignored.

## Streaming

While Deepgram is producing interim results the bot calls `sendMessageDraft`. Drafts are a 30-second preview and are often ignored in groups. The durable result is always a `sendMessage` (or a `receiver_user_id` private send for `/vp`).

## Cache

The same `file_id` is served from `voicetotext.transcripts` without a new Listen call.
