# Telegram Integration

## Commands

| Command | Surface | Behavior |
|:--|:--|:--|
| `/start` | DM | Owner-scoped family panel |
| `/start` | group | Ephemeral panel |
| `/v` | reply in group | Public transcript in the same topic |
| `/vp` | ephemeral reply in group | Requester-only transcript |
| `/language [ru\|en]` | DM | Open or change shared interface language |
| `/stats` | DM | Personal stats with a global tab |
| `/help`, `/about` | DM | Help and project details |

Bare group media and non-ephemeral group `/vp` stay silent. Unknown group
commands stay silent. Menu callbacks include the owner ID and foreign taps only
receive a short callback toast.

## Delivery

Public and DM transcripts use Bot API `sendRichMessage` with a
`rich_message.html` payload. The rendered transcript is HTML, and
`InputRichMessage` accepts exactly one of `html`, `markdown`, or `blocks`; the
`markdown` field would additionally parse GitHub-flavored Markdown and turn
speech containing `*`, `_`, `#`, `|`, or list-like lines into formatting. Reply
and topic identifiers are preserved. The Bot API limit is 32,768 UTF-8
characters, so longer text is split at a paragraph or word boundary into
independently valid, numbered rich messages. No transcript is sent as a
document.

In a direct chat Voicy answers a cache miss with a "Transcribing…" message
that replies to the voice, keeps a typing indicator alive while it works, and
then edits that same message into the transcript. A cache hit skips the
placeholder entirely. Longer transcripts continue from the edited message.

For `/vp`, Voicy immediately creates an ephemeral placeholder, sent with the
Bot API 10.3 `ephemeral_message_parameters` object. Telegram accepts a new
ephemeral message only within 15 seconds of the command that triggered it, and
transcription always outlives that window, so the result is delivered by
editing the placeholder: `editEphemeralMessageText` carries a `rich_message`
and therefore the full 32,768-character transcript. A transcript that needs
more than one message goes to the requester's DM. If the user has not opened
the bot, the placeholder receives an owner-bound `/start transcript_<token>`
deep link.

## Cache and Settings

The cache key is `(file_id, variant)` in `voicy.transcripts`. The variant
contains only Deepgram-affecting settings, so delivery formatting does not
duplicate recognition work.

| Key | Default | Effect |
|:--|:--|:--|
| `smart_format` | on | Punctuation, numbers, dates |
| `paragraphs` | on | Paragraph breaks |
| `filler_words` | off | Keep filler words |
| `profanity_filter` | off | Mask profanity |
| `diarize` | off | Speaker turns using `diarize_model=latest` |
| `quote` | on | Render transcript as a blockquote |
| `meta` | off | Show language, duration, and confidence |

Settings callbacks carry `set:<key>:<0|1>` and are idempotent. Interface
language is shared through Core and falls back to the Telegram profile hint.

## Message Safety

Classic menu panels use Telegram HTML. Transcript and user-controlled text is
escaped before it enters the rich HTML payload or HTML wrappers. Errors shown to users
never include raw upstream payloads, tokens, keys, or full Bot API URLs.
