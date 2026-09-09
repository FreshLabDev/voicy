# Changelog

All notable Voicy changes are documented here.

The `## <tag>` section of this file *is* the GitHub Release body: the release
workflow copies it verbatim and refuses a tag that has no section. Write it
for whoever has to decide whether to upgrade.

See [`docs/versioning.md`](docs/versioning.md) for what the numbers mean and
[`docs/releases.md`](docs/releases.md) for how a release is published.

## Unreleased

Use this section for changes that are merged but not released yet.

## v0.0.2-alpha.1 - 2026-09-09

### Added

- **The language screen offers "Follow Telegram".** Picking a language by hand
  records a manual choice in the shared Core hub, and manual outranks every
  automatic source for ever — so somebody who tapped the wrong flag once read
  Voicy in that language until a human edited the database. The button clears
  that choice and lets the Telegram client's own `language_code` decide again.
  The database role now also needs `EXECUTE` on `core.clear_language`.

### Changed

- **Every panel is one shape.** Help, Language, Settings and About each built
  their own HTML — Help on the argument that a bulleted list is already a shape,
  About because the version had to sit next to the name. They all go through the
  one panel helper now: bold title, italic one-line hint, substance in a quote.
  The About card still leads with `Voicy · <version>` and the language screen
  still says nothing its sixteen buttons already say.

- **One colour, one meaning.** The open statistics tab was Primary, the colour
  that marks the single thing a person came to do; it reports which numbers are
  showing, so it is Success now, and Primary is left to the one button that
  leads anywhere — Language on the direct-chat home. Both statistics tabs carry
  a state glyph, not just the open one, and every switch that is on is Success
  like the current language is. Seven coloured switches are loud; a colour that
  means one thing on one screen and another on the next is worse.

## v0.0.1 - 2026-09-09

Everything that shows or configures now lives behind /start, the panels stopped
wearing one shape that fitted none of them, and a callback can no longer act on
a message it was never shown.


### Changed

- A panel callback only acts on a message it is entitled to act on. Telegram
  lets a client send any callback data for any message it can see, not only the
  buttons it was shown; the owner id inside the data stopped one person driving
  another's panel, but not somebody forging their own id against a public
  message. In a group Voicy's public messages are transcripts, so `close` could
  have deleted someone else's, and opening a tab could have overwritten one.
  Ephemeral messages are unaffected — they are already visible to one person —
  and a direct chat is that person's own.

- **The direct-chat command menu is one entry: `/start`.** `/stats`,
  `/language`, `/help` and `/about` were published as commands while already
  being buttons on the `/start` panel, so every one of them listed the same door
  twice. Everything that shows or configures now lives behind `/start`; a
  command is published only when it acts on a message somebody points at, which
  is what `/v` and `/vp` do and why the group menu is unchanged. All four still
  answer for anyone who types them from memory — they are simply no longer
  advertised.
- **The panel is two screens instead of one.** In a direct chat it keeps every
  tab and no longer offers `Close`: there the whole conversation is the panel,
  and closing it deleted the message the reader was looking at. In a group it
  offers `Close`, because there the panel is Voicy's message sitting in somebody
  else's feed, and it now shows only the about card and a line explaining `/v`
  and `/vp`. Settings and the interface language are personal and shared with the
  sibling bots through Core, so a group visitor is no longer offered switches
  that do not mean in a group what they appear to mean.
- **Each panel now carries the shape its content asks for.** One "title,
  subtitle, quoted body" frame was stretched over five screens of different
  kinds, and because the frame required a subtitle, subtitles were invented for
  screens that had nothing to put there — "About" was captioned "Voicy". Help is
  a list and is no longer boxed in a quote; the language screen is a heading and
  one sentence over the picker; settings is a heading and the note the toggles
  cannot show; the home card keeps the full frame, which is the one place it was
  always right.
- **`About` is the family-standard card.** It leads with the product and the
  build it is actually running — the same version string `/healthz` reports — one
  line of what Voicy does, then `label · value` rows: the recognition engine, the
  repository as a link in the text under Apache-2.0, and the admin to write to.
  The repository is deliberately a link and not a button: two ways to open one
  address are not two actions. Localized in all sixteen languages.
- **Button styles mark one thing per screen** rather than four scattered
  accents. The language tab is highlighted on the direct-chat home, since nothing
  else there is readable until the language is right; the language currently in
  use is marked in the grid of sixteen; the open statistics tab is marked; and
  `Close` is destructive. The seven-switch settings grid stays unstyled — its
  state is already on the glyphs, and colouring all of it would highlight none of
  it.
- One versioning and release document for the whole family. `docs/versioning.md`
  and `docs/releases.md` are now byte-identical across every Asterfield
  repository apart from two clearly marked sections: this repository's own
  version line, and the surface where a change here breaks something. They spell
  out what each of the three numbers means, what the `-alpha.N` suffix counts,
  when alpha becomes beta and when it is legitimate to skip to rc or run a
  pre-release in production.
- **Pre-releases are now tagged on `dev`, not `main`.** Only stable versions are
  tagged on `main`, on the merge commit from `dev`, so `main` answers exactly one
  question: what is in production. The test bot runs `dev`, the production bot
  runs `main`. `release.yml` enforces this and refuses a tag on the wrong branch.
  Earlier pre-releases in this repository were tagged on `main` under the
  previous rule; they are left as they are.

### Removed

- Translation keys that nothing renders any more: `cmd.stats`, `cmd.language`,
  `cmd.help` and `cmd.about` with the commands they described, `about.title`,
  `about.hint` and `about.body` with the old three-part About panel, and
  `help.hint`, `lang.hint` and `settings.hint` — the subtitles that only restated
  their own title or their own body.

## v0.0.1-beta.4 - 2026-09-08

### Fixed

- `voicy_telegram_errors_total` no longer counts the startup capability probe.
  The probe calls a method with an empty body on purpose and is answered with
  a parameter error, so every restart recorded two Telegram errors before the
  bot had served anything.

## v0.0.1-beta.3 - 2026-09-08

Everything a review of the shared Telegram client turned up, including two
ways the bot token could have escaped.

### Security

- Through `tg` v0.0.1-alpha.6: the bot token can no longer reach a log line
  through an error body echoed by something in front of the Bot API server, nor
  through the file path wrapped inside a local-file error. A symlink in the
  server's media directory can no longer read a file out of another bot's
  directory.

### Fixed

- Through the client: an `ok=false` answer carried on a 2xx is an API error
  again, so rate limits and edit-not-modified are classified rather than
  slipping through as untyped failures; a startup preflight waits through a
  Bot API server that is still booting instead of exiting on its first 502;
  and `TELEGRAM_READY_WAIT` now bounds the whole startup check rather than only
  the pauses inside it.

## v0.0.1-beta.2 - 2026-09-08

Voicy's Telegram client is no longer its own, and it will not start against a
server that cannot serve it.

### Changed

- Telegram now goes through `github.com/FreshLabDev/tg`, the client shared by
  the bot family, and `internal/telegram` is gone. The transport, retries,
  token redaction, rich messages and the 10.3 ephemeral contract were the same
  code in three bots and had already drifted apart; they now have one home.
  Nothing about Voicy's behaviour changes with them.
- `internal/media.Of` replaces `telegram.Message.Media()`. Go cannot put a
  method on another package's type, and the order in which attachments are
  preferred — speech first — was always a Voicy decision rather than a
  Telegram one.
- `voicy_telegram_errors_total` now also counts transport failures (DNS,
  connect, reset). They were invisible before, because only a parsed API error
  reached the counter.

### Added

- `docs/releases.md` gained a **Deploying** section, and `AGENTS.md` points at it.
  Releasing was documented; deploying was not, in any repository in the family —
  the process stopped at "deploy it" and never said how. That gap mattered more
  after the stacks moved from building on the host to pulling a published image,
  because the procedure changed on the same day. The section names this stack's
  host directory, its env file, the variable that selects the image, the networks
  it needs, and what a rollback actually is.

- A preflight at startup. Voicy names the methods it cannot work without —
  `sendRichMessage` and `editEphemeralMessageText` — and refuses to start when
  the Bot API server does not implement them. A server behind the bot answers
  `404 method not found` to every rich message, which used to mean a bot that
  polled happily and answered nothing at all.
- `BOT_API_FILES_DIR`, required when `TELEGRAM_API_BASE` is self-hosted, and
  `TELEGRAM_READY_WAIT` (default 30s) for a server that is still booting.

### Fixed

- Media on a self-hosted Bot API server is read from disk again. Such a server
  runs with `--local`, and a `--local` server serves no files over HTTP at all:
  its `/file/bot<token>/…` route answers 404 in every version. Fetching over
  the network, introduced in v0.0.1-alpha.10, could therefore never work. Only
  this bot's own directory is mounted — the parent holds one directory per bot
  named after that bot's token — and the mount is verified at startup instead
  of failing on the first voice message.

### Operations

- The WS04 stack mounts the bot's own media directory on the Bot API server
  and runs as uid 101, the user that server writes as. `BOT_API_HOST_DIR` and
  `BOT_API_FILES_DIR` name that directory on the host and in the container.
  Files are removed after transcription, which a local server never does on
  its own.
- The mount needs Compose's long volume syntax *with* a `bind` option. Both
  paths contain the bot token, a token contains a colon, and Compose flattens
  an option-less long mount back into `source:target:rw`, which the daemon
  then splits in the wrong places.
- Production runs against `telegram-bot-api-next`, the Asterfield-built Bot API
  10.3 server, while the older shared server keeps the bots that have not
  moved. Moving a bot between servers is: stop it, `logOut` on the server it
  leaves, then start it on the new one. A running bot re-registers itself
  within a second, so the stop is not optional.

## v0.0.1-beta.1 - 2026-09-06

Voicy speaks the sixteen languages the bot family shares, transcribes audio
files and video rather than only voice messages, and no longer holds a
recording in memory to do it.

### Added

- Sixteen interface languages, matching the set searchy and vido offer. Strings
  moved out of Go into `internal/i18n/translations.json`, and the picker shows
  each language by its own name. Ukrainian and Belarusian used to collapse into
  Russian, which also meant Voicy overwrote a person's real choice in the shared
  Core language hub with a coarser one.
- Command descriptions are published per language through `setMyCommands`, so
  the command menu is no longer English for everyone.
- Audio files, video and documents are transcribed, not just voice messages and
  circles. In a direct chat, voice, circles and audio files are still handled
  without being asked; video and documents wait for an explicit `/v`, so a
  Deepgram call is never spent on a file shared for some other reason. A
  document is accepted on its MIME type or file name and refused with a plain
  answer when it carries no sound.
- ffmpeg reduces video, and any media above `EXTRACT_ABOVE_BYTES`, to a 16 kHz
  mono Opus track before Deepgram sees it. This is what Deepgram recommends for
  large video, and it keeps a large upload from crossing the network twice.
- `FFMPEG_PATH` (default `ffmpeg`, empty disables extraction),
  `EXTRACT_ABOVE_BYTES` (default 20 MB) and `MEDIA_TMP_DIR`.
- Statistics count files as their own category next to voice messages and
  circles.

### Changed

- Media is streamed to a temporary file and read from there, instead of being
  buffered whole in memory. That buffering, not Deepgram, was what capped
  `MAX_MEDIA_BYTES`: with several transcriptions running at once the ceiling
  was memory. Deepgram's own limit is 2 GB, and the Deepgram request body is now
  streamed from disk too, so a retry can replay a body of any size.
- The Telegram client exposes `DownloadToFile` rather than returning bytes.

### Migrations

- `migrations/005_media_kinds.sql` widens the transcript kind constraint to the
  new media types and adds `user_stats.file_count`.

### Operations

- The image now installs ffmpeg. Without it, video is sent to Deepgram
  unextracted and may not transcribe at all; the bot logs a warning at startup.
- `MAX_MEDIA_BYTES` can be raised well beyond the previous memory-bound value
  when a self-hosted Bot API server is in use.

## v0.0.1-alpha.10 - 2026-09-06

Corrects how Voicy reads media from the self-hosted Bot API server, before that
path is used in production.

### Fixed

- Files from a local Bot API server are fetched over the internal network
  instead of a shared volume. The server's data directory holds one
  subdirectory per bot, named after that bot's token, so mounting it would have
  given Voicy every other bot's credentials. An absolute `file_path` is made
  relative again and requested from the same server, which is what actually
  lifts the 20 MB ceiling. Reading from disk is kept for a deployment that does
  mount the directory, and a file already rejected as oversize is no longer
  downloaded a second time over HTTP.

### Operations

- The WS04 stack joins `telegram_bot_api_net` and mounts nothing.

## v0.0.1-alpha.9 - 2026-09-06

Operations release. Voicy can run against the fleet's self-hosted Bot API
server, exposes process counters, proves its SQL against a real PostgreSQL, and
can no longer send the same transcript twice.

### Added

- `TELEGRAM_API_BASE` selects the Bot API server, defaulting to Telegram's own.
  Pointed at the shared self-hosted server, `getFile` is no longer capped at
  20 MB. That server runs with `TELEGRAM_LOCAL` and answers with an absolute
  path in its data directory, so Voicy reads the bytes from the mounted volume
  and deletes the file afterwards: a local server never reclaims them itself.
- `/metrics` serves process counters in Prometheus text exposition format,
  covering what never becomes a job row: polling failures, cache hits and
  misses, Deepgram retries, Telegram rate limits and errors by method, job
  failures by stage, and audio seconds sent.
- Database tests that run against a real PostgreSQL, covering job idempotency,
  the atomic completion-and-statistics transaction, cache variants, the stale
  job reaper, owner-bound deep links, and retention. They require a disposable
  database whose name contains `test` and refuse to run anywhere else. CI runs
  them against a PostgreSQL 17 service, and a release is now gated on them.

### Fixed

- A transcript is delivered at most once per update. Delivery and the terminal
  transition are two writes, and a database failure between them made the
  update retry and send the same text again. Delivery is now recorded on the
  job as soon as it succeeds, and a retry closes the job instead of resending.

### Migrations

- `migrations/004_job_delivery.sql` adds `jobs.delivered_at`.

### Operations

- `TELEGRAM_API_BASE` is optional and defaults to the cloud server, so an
  existing deployment is unaffected until it is set.
- Moving a token to a self-hosted server requires calling `logOut` on the cloud
  server first. Telegram then refuses to log back in to the cloud for ten
  minutes, so the switch back is not instant.
- The WS04 stack now also joins `telegram_bot_api_net` and mounts the Bot API
  server's data directory.

## v0.0.1-alpha.8 - 2026-09-06

Throughput release. Voicy now transcribes for several people at once, survives a
flaky Deepgram, stops rescanning the jobs table for every statistics tap, and
shows the user that something is happening.

### Changed

- A poll batch is handled by a pool of workers instead of one update at a time.
  A single long recording used to block every other user in every other chat.
  Updates from the same person still run in arrival order on one worker, and the
  poll offset advances only once the whole batch is finished, so an in-flight
  update is never confirmed to Telegram.
- A direct chat answers a cache miss immediately with "Transcribing…", keeps the
  typing indicator alive while it works, and edits that message into the
  transcript. A single chat action expires after five seconds, so long
  recordings previously showed nothing at all. A cache hit skips the
  placeholder.
- Statistics are served from an in-memory snapshot cache with a background
  refresh. The peak-hour histogram scans the jobs table, and the My/All tabs are
  one tap apart, so idle toggling was a stream of full scans.
- Telegram and Deepgram share one tuned HTTP transport. Go's default of two idle
  connections per host would have serialized the new concurrency.

### Fixed

- Deepgram requests are retried on 408, 429, and 5xx, with jittered backoff and
  respect for `Retry-After`. Production answered the same video circle with
  HTTP 408 twice and gave up both times.
- A Deepgram request deadline now scales with the length of the audio. The fixed
  two-minute client timeout could not be met at `MAX_MEDIA_DURATION=1h`.
- A failed Deepgram call reports the `dg-request-id` and Deepgram's own reason
  instead of a bare status code.
- A failure in a direct chat replaces the placeholder rather than leaving
  "Transcribing…" stranded above a separate error message.

### Added

- `MAX_CONCURRENT_JOBS` (default `4`) bounds how many updates are handled at
  once.
- `STATS_CACHE_TTL` (default `5m`) and `STATS_TIMEZONE` (default `Europe/Kyiv`),
  which was previously hardcoded in the peak-hour query.

### Migrations

- `migrations/003_stats_index.sql` adds two partial indexes on `jobs` for sent
  rows, so the peak-hour query stops being a sequential scan.

### Operations

- All three new variables are optional and default to today's behavior.
- The migration is two `CREATE INDEX IF NOT EXISTS` statements and is safe to
  apply on a running deployment.

## v0.0.1-alpha.7 - 2026-09-06

Voicy now speaks the Bot API 10.3 ephemeral contract, sends transcripts as rich
HTML instead of Markdown, and recovers on its own from interrupted jobs and
database blips.

### Fixed

- Ephemeral messages are sent with `ephemeral_message_parameters`. Bot API 10.3
  removed the flat `receiver_user_id` parameter, which Telegram ignores: a `/vp`
  placeholder could be delivered to the whole group instead of the requester.
- Transcripts are sent in `rich_message.html` rather than `rich_message.markdown`.
  The payload has always been HTML, and the Markdown field additionally parses
  GitHub-flavored Markdown, so speech containing `*`, `_`, `#`, `|`, backticks,
  or list-like lines was reformatted.
- A long `/vp` result now edits the ephemeral placeholder with a `rich_message`.
  Telegram accepts a new ephemeral message only within 15 seconds of the
  triggering command, which transcription always outlives, so the previous
  ephemeral rich reply could never succeed.
- Jobs abandoned by a crash are failed by a reaper after `JOB_STALE_AFTER`.
  Nothing else moved a `received` row to a terminal state, so one interrupted
  transcription pinned `/healthz` at 503 and the container at unhealthy.
- The first PostgreSQL connection is retried with backoff instead of exiting.
  A container DNS lookup that is not ready at start no longer restarts the bot.
- A failed offset or job-status write is logged and polling continues instead of
  terminating the process on a transient database error.
- A transcript cache variant now describes the request that was actually sent.
  Diarization implies paragraphs, and the variant records that.
- A send rejected with `migrate_to_chat_id` is retried against the new
  supergroup, so a chat upgraded mid-request still receives its transcript.
- `link_preview_options` replaces the removed `disable_web_page_preview`
  parameter, `sendChatAction` carries the forum topic, and the deep-link
  transcript touch no longer refreshes every variant of a file.
- Update retries are spread with real jitter; the helper previously returned its
  input unchanged.

### Added

- `JOB_STALE_AFTER` (default `30m`) sets when an unfinished job is failed and
  when `/healthz` reports it as stuck. Both use the same threshold.
- `my_chat_member` updates are classified and recorded. They were requested in
  `allowed_updates` and then discarded. Groups stay quiet.

### Operations

- No migration. `JOB_STALE_AFTER` is optional and defaults to `30m`.
- Existing cached transcripts stay valid. Only diarized settings produce a
  different variant string, and those rows are recomputed on next use.

## v0.0.1-alpha.6 - 2026-08-22

### Operations

- Updated pinned Docker release actions to their Node 24 generations, removing
  GitHub's Node 20 deprecation annotations from the release workflow.

## v0.0.1-alpha.5 - 2026-08-22

### Fixed

- Repeated ordinary and ephemeral panel actions now treat Telegram's
  `message is not modified` response as an idempotent success instead of
  retrying the update and eventually dropping it.

## v0.0.1-alpha.4 - 2026-08-22

Voicy now has one canonical name from repository folder to production schema,
reliable Telegram initialization and job accounting, Rich Markdown transcript
delivery, complete settings, and useful personal and global statistics.

### Added

- `/start` panel with Language, Settings, Stats, Help, About, and Close.
- `/language [ru|en]` in DM opens the language panel or sets the language
  across the shared Core language hub.
- Settings for smart formatting, paragraphs, filler words, profanity filter,
  diarization, quote style, and metadata.
- Per-user Deepgram options: `smart_format`, `paragraphs`, `filler_words`,
  `profanity_filter`, `diarize_model=latest`. `detect_language` and
  `mip_opt_out` remain always-on.
- Localized speaker turns from Deepgram paragraph-level speaker metadata.
- Personal and global statistics tabs with users, transcript kinds, audio
  duration, words, Kyiv peak hour, and last detected language.
- Owner-bound deep-link recovery for a long ephemeral transcript when neither
  ephemeral Rich Markdown nor direct-message delivery is available.
- Media size and duration limits, hourly retention cleanup, and stuck-job
  health reporting.

### Changed

- Removed Deepgram WebSocket code and Telegram draft delivery. Prerecorded
  `POST /v1/listen` is the only STT transport.
- Removed transcript-document delivery and its setting. Transcripts use Bot API
  `sendRichMessage`, preserving replies and topics, with safe splitting above
  32,768 characters.
- Settings callbacks carry the desired value and are safe to retry.
- Telegram startup retries `deleteWebhook`, `getMe`, and command registration
  until all succeed. `/healthz` remains unhealthy before initialization.
- A terminal job transition and user-stat update now commit atomically and only
  once. Failed and empty jobs never affect user-facing statistics.
- `transcripts` primary key is `(file_id, variant)` and cached speaker turns are
  stored with the text.
- The folder, stack, database schema, Core bot key, role, and container are all
  named `voicy`.

### Migrations

- `002_user_settings.sql` adds settings, cache variants, speaker turns,
  retrieval tokens, and last-use timestamps.
- Core `011_voicy.sql` migrates legacy presence and language rows, renames the
  schema and role, and narrows `voicy_core` to the three shared API functions it
  uses.

### Operations

- Updated to Go 1.26.6 and fixed reachable standard-library and `x/text`
  vulnerabilities.
- CI and release workflows pin actions, pin `govulncheck`, run full release
  verification, require tagged code to be on `main`, fail on missing changelog,
  and publish an immutable image digest with SBOM and provenance.
- Production Compose accepts only an explicit `VOICY_IMAGE` reference.

## v0.0.1-alpha.3 - 2026-08-14

Menus and transcripts follow the family panel contract. Long voices keep
Deepgram paragraph breaks.

### Added

- Menu callbacks are owner-scoped (`m:<user_id>:<action>`). A foreign tap is
  ignored with a short toast.
- Close deletes the panel instead of replacing it with a checkmark.
- Empty stats show a short empty state, not zeros.
- Deepgram Listen requests `paragraphs=true`. The formatted reply uses that
  paragraph text when Deepgram returns it.

### Changed

- `/start`, Help, and Stats use `<b>title</b>` + `<i>hint</i>` +
  `<blockquote>` body, with Back/Close on subpanels.
- Transcripts are a clean blockquote. Language, duration, and confidence are
  no longer appended.

### Known Limitations

- Cached transcripts from earlier alphas stay a single paragraph until that
  `file_id` is transcribed again.

## v0.0.1-alpha.2 - 2026-08-14

File uploads now go to Deepgram prererecorded Listen instead of Live streaming.

### Changed

- `Transcribe` sends the downloaded Telegram file to `POST /v1/listen`. Default
  `Content-Type` is `audio/ogg` when Telegram does not provide one.
- Live WebSocket streaming remains in the client but is no longer the file
  upload path.

### Fixed

- Empty or mis-detected audio containers are less likely to fail Deepgram
  because files are sent as prererecorded OGG rather than a raw stream.

## v0.0.1-alpha.1 - 2026-08-14

First public alpha. A Go Telegram bot that turns a voice message or video circle
into text with Deepgram.

### Added

- DM transcription: send a voice or video circle and receive the text.
- Group `/v` replies with a public transcript of the replied voice or circle.
- Group `/vp` is an ephemeral command: a private placeholder inside Telegram's
  15-second window, then `editEphemeralMessageText`. DM fallback if the edit
  fails. Ordinary non-ephemeral group `/vp` stays quiet.
- Deepgram Live streaming (`nova-3`, `language=multi`) with prererecorded
  Listen fallback. Interim text uses Telegram `sendMessageDraft` where the
  client accepts drafts.
- `file_id` cache: store Telegram file id plus transcript, never audio bytes.
  The same file is not sent to Deepgram again.
- First-party Telegram HTTP client. No third-party Bot API SDK.
- Shared `core` identity via `core.touch`. Domain tables live in `voicy.*`.
- User-facing stats count only successful non-empty transcripts. Empty and
  failed runs are logged only.
- `/healthz`, Docker Compose for local development, and a WS04 production
  compose file that joins `core_net`.

### Operations

- Local Compose seeds a minimal `core` schema (`deploy/core-init.sql`).
- Production uses `voicy_core` on shared `core-postgres`
  (`deploy/ws04/compose.yaml`).

### Known Limitations

- Alpha: live validation is limited. Group drafts are often ignored by Telegram;
  `/v` still sends a public final message.
- Privacy mode stays on. Groups do not auto-transcribe every voice.
- Deepgram streaming of an already-downloaded file falls back to REST Listen
  when the WebSocket path fails.

### Migrations

- `001_init.sql` creates `transcripts`, `jobs`, `user_stats`, and
  `runtime_state` in the `voicy` schema.
