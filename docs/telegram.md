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
`rich_message.markdown` payload. Reply and topic identifiers are preserved.
The Bot API limit is 32,768 UTF-8 characters, so longer text is split at a
paragraph or word boundary into independently valid, numbered Rich Markdown
messages. No transcript is sent as a document.

For `/vp`, Voicy immediately creates an ephemeral placeholder. Results up to
4,096 characters edit it directly. Longer single-part results use an ephemeral
Rich Markdown reply and remove the placeholder. If Telegram rejects that path,
Voicy tries the requester's DM. If the user has not opened the bot, the
placeholder receives an owner-bound `/start transcript_<token>` deep link.

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
escaped before it enters Rich Markdown or HTML wrappers. Errors shown to users
never include raw upstream payloads, tokens, keys, or full Bot API URLs.
