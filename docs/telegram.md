# Telegram Integration

## Commands

`/start` opens the menu in DM. In groups and supergroups it is registered as a
Bot API 10.2 ephemeral command: the prompt is visible only to the invoking
user. An ordinary non-ephemeral group `/start` gets no public response.

| Command | Where | Purpose |
|:--|:--|:--|
| `/start` | private | Inline menu (stats, help, close) |
| `/start` | groups | Ephemeral DM prompt |
| `/v` | groups (and DM) | Transcribe the replied voice or circle for everyone |
| `/vp` | groups (ephemeral) and DM | Transcribe only for the requester |
| `/stats` `/help` | private, also from the menu | Stats and help panels |

Only `/start` is published for private chats. Groups see `/start` (ephemeral),
`/v`, and `/vp` (ephemeral). A command is handled only when it is bare or
addressed to this bot. An unknown command nudges the user in private chats
only.

All setup happens through inline keyboards or a voice/circle. Other text in a
group is ignored so the bot remains quiet.

## Groups

A voice or video circle without `/v` or `/vp` is ignored. Privacy mode stays
on, so Voicy does not see every voice in the chat.

`/v` replies in the same chat and topic with a public transcript.

`/vp` is ephemeral. Voicy immediately replies to the incoming
`ephemeral_message_id` with a placeholder so Telegram's 15-second window is not
spent on Deepgram. After transcription it calls `editEphemeralMessageText`. If
that edit fails, the result is sent in DM. A late `sendMessage` with only
`receiver_user_id` is not used: Bot API 10.2 allows that solely for chat-admin
bots.

## Streaming

While Deepgram is producing interim results Voicy calls `sendMessageDraft`.
Drafts are a short-lived preview and are often ignored in groups. The durable
result is always a normal send (`/v`, DM) or an edited ephemeral message
(`/vp`).

## Cache

The same Telegram `file_id` is served from `voicetotext.transcripts` without a
new Listen call. Voicy stores the id and the text. The audio file stays on
Telegram.

## Message Style

UI panels use classic HTML `sendMessage` / `editMessageText` with link previews
disabled. User-controlled and transcript text is HTML-escaped. Errors shown to
the user do not include raw Telegram or Deepgram API strings.
