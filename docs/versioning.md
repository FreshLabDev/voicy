# Versioning

Voicy uses SemVer-style versions. This first line starts at `v0.0.1`. Alpha,
beta, and rc tags are reserved for changes that still need limited live
validation.

## Version Line

```text
v0.0.1-alpha.1  first public alpha
v0.0.1-alpha.2  REST Listen for Telegram file uploads
v0.0.1-alpha.3  family panels, owner callbacks, Deepgram paragraphs
v0.0.1-alpha.4  Rich Markdown, reliable jobs, settings, Core rename
v0.0.1-alpha.5  idempotent Telegram panel edits after live validation
v0.0.1-alpha.6  Node 24 release actions after GitHub workflow validation
v0.0.1-alpha.7  Bot API 10.3 ephemeral contract, rich HTML, self-healing jobs
v0.0.1-alpha.8  concurrent updates, Deepgram retries, cached statistics
v0.0.1-alpha.9  self-hosted Bot API, Prometheus metrics, delivery guard
v0.0.1-beta.1   live-tested with limited users
v0.0.1-rc.1     public release candidate
v0.0.1          first public MVP on this line
```

Work happens on `dev`. Every pre-release and stable release is published from
`main`. The `## Unreleased` changelog section tracks what has merged to `dev`
but is not yet tagged.

After this line:

```text
v0.0.x          backward-compatible fixes on the 0.0.1 contract
v0.1.0          notable MVP-compatible product or operations improvements
v1.0.0          first stable production contract
```

## Rules

- Use `alpha` until DM transcription, group `/v` `/vp`, cache hits, and restart
  recovery are proven with real Telegram and Deepgram credentials.
- Use `beta` after the complete bot works end to end for limited users.
- Use `rc` when only release-blocking fixes are expected.
- Use patch versions for fixes that do not change product behavior or runtime
  assumptions.
- Use minor versions for visible UX improvements or operational improvements
  that remain in MVP scope.
- Do not use `v1.0.0` until the production contract, shared-core registration,
  and retention policy are stable.

## Breaking-Sensitive Areas

Breaking changes must be explicit when they affect:

- required environment variables
- PostgreSQL schema or migration requirements
- Deepgram Listen request shape
- group `/v` and `/vp` semantics
- `file_id` cache behavior
- Docker Compose or deployment assumptions
