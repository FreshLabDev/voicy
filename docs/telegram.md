# Telegram Integration

## Commands

| Command | Surface | In the menu | Behavior |
|:--|:--|:--|:--|
| `/start` | DM | yes | Owner-scoped family panel |
| `/start` | group | yes, ephemeral | Ephemeral group panel |
| `/v` | reply | yes | Public transcript of any audio or video, in the same topic |
| `/vp` | ephemeral reply in group | yes, ephemeral | Requester-only transcript |
| `/language [ru\|en]` | DM | no | Opens the language tab, or sets the language directly |
| `/stats` | DM | no | Opens the statistics tab |
| `/help` | DM | no | Opens the help tab |
| `/about` | DM | no | Opens the about card |

Everything that shows or configures lives behind `/start`. A separate command is
published only when it acts on a message somebody points at, which is why `/v`
and `/vp` are in the group menu and nothing else is in the direct-chat one:
statistics, language, help and about are tabs of the panel, and publishing a
command that only opens a tab lists the same door twice. The four still answer
for anyone who types them from memory.

Bare group media and non-ephemeral group `/vp` stay silent. Unknown group
commands stay silent. Menu callbacks include the owner ID and foreign taps only
receive a short callback toast.

## Panels

The panel is two screens. In a direct chat it carries every tab — language,
statistics, settings, help, about — and offers no Close, because there the whole
conversation is the panel and deleting the message the reader is looking at
leaves them with their own history. In a group it carries the about card and
Close, and its text explains `/v` and `/vp`: settings and the interface language
are personal and shared with the sibling bots through Core, so offering them from
somebody else's group would promise a local effect Voicy does not have.

Every screen is one shape, built by `transcript.Panel`: a bold title, an italic
one-line hint, and the substance in a quote. Nothing assembles its own HTML, so
no screen can drift into a shape of its own.

Button styles (Bot API 9.4+) each mean one thing. Primary marks the single thing
a person most likely came to do, so there is at most one per screen and only one
in the whole panel: the language tab on the direct-chat home, since nothing else
there is readable until the language is right. Success reports the state the
reader is in and never sits on a button that acts: the language currently in
use and the open statistics tab, both of which do nothing when tapped again.
The settings switches are the near miss — a switch does report its state, but
the same tap turns it off, so it would be the colour for "this is how things
are" on the control that undoes it. Those keep the glyph and take no colour.
Danger destroys, which here is only Close. Options carry `◉`/`◎` in both states
so a set has one left edge.

The language screen ends with **Follow Telegram**. Picking a language by hand
writes a manual observation to Core and manual outranks every automatic source
for ever, so without it a wrong tap would be permanent; it calls
`core.clear_language` and lets the client's own `language_code` decide again.

## Media

Voice messages, video circles and audio files are transcribed in a direct chat
without being asked. Videos and documents are not: answering every file with a
Deepgram call would be surprising and expensive, so they wait for an explicit
`/v`. A document is accepted when its MIME type or file name looks like audio
or video, and refused with a plain answer otherwise.

Media is streamed to a temporary file and never held whole in memory. Video, and
any other media above `EXTRACT_ABOVE_BYTES`, is reduced to a 16 kHz mono Opus
track by ffmpeg before Deepgram sees it. Deepgram recommends exactly that for
large video, and it keeps a large upload from crossing the network twice. Both
the download and the extracted track are deleted when the job ends.

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
Voicy renders the sixteen languages the fleet shares, so a preference set in
searchy or vido is honoured here rather than collapsed into Russian or English.
Command descriptions are published per language through `setMyCommands`.

## Message Safety

Classic menu panels use Telegram HTML. Transcript and user-controlled text is
escaped before it enters the rich HTML payload or HTML wrappers. Errors shown to users
never include raw upstream payloads, tokens, keys, or full Bot API URLs.
