# Release Process

This document explains how Voicy uses `CHANGELOG.md` and GitHub Releases.

## Changelog Rules

- Keep `CHANGELOG.md` as the source of truth for human-readable release history.
- Put unreleased user-visible, operational, security, migration, or behavior
  changes under `## Unreleased`.
- Do not record every small refactor. Record changes that matter to users,
  operators, contributors, or future release decisions.
- Use these sections when relevant:
  - `Added`
  - `Changed`
  - `Fixed`
  - `Security`
  - `Migrations`
  - `Breaking`
  - `Known Limitations`
- Keep entries short and concrete.
- Mention migration filenames in `Migrations`.
- Mention required environment variable or deployment changes explicitly.

## Preparing A Release

1. Finish code and documentation changes on `dev`.
2. Run the verification commands from `AGENTS.md`.
3. Run a real smoke test for `beta`, `rc`, and public releases.
4. Merge to `main`.
5. Move relevant `Unreleased` entries into a version section:

   ```text
   ## v0.0.1-alpha.1 - 2026-08-14
   ```

6. Keep an empty `## Unreleased` section at the top for future changes.
7. Write release notes from the version section.
8. Create an annotated git tag on `main`.
9. Create a GitHub Release.

## GitHub Release Notes

Use this shape for release notes (same sections as `CHANGELOG.md`):

```text
### Added

- Short concrete bullets from the version section of CHANGELOG.md.

### Operations

- Migration notes, env, or deploy notes when relevant.
```

GitHub Release **title** is the version only (`v0.0.1-alpha.1`), not
`Voicy v…`. Copy the matching `CHANGELOG.md` version section into the release
body (skip the `## vX.Y.Z` heading).

For `alpha`, `beta`, and `rc` versions, mark the GitHub Release as pre-release.
For stable tags, publish a normal GitHub Release.

## Commands

Create a pre-release:

```sh
git tag -a v0.0.1-alpha.1 -m "v0.0.1-alpha.1"
git push origin main
git push origin v0.0.1-alpha.1
gh release create v0.0.1-alpha.1 \
  --prerelease \
  --title "v0.0.1-alpha.1" \
  --notes-file release-notes.md
```

Do not publish a release before the release notes, tag, and verification status
all match.

## Production Deployment

Production runs the `voicy` stack from `/opt/stacks/voicy` and uses the shared
`core-postgres` role `voicy_core`. Core migration 011 must be applied before a
Voicy version that enforces `search_path=voicy` starts. The root
`docker-compose.yml` is local-development configuration and must not be used as
a production deployment source. The WS04 file is `deploy/ws04/compose.yaml`.

After the tag workflow publishes the image, resolve its digest and put that
immutable `ghcr.io/freshlabdev/voicy@sha256:...` value in `VOICY_IMAGE`. Never
deploy a mutable tag.

```sh
WS04_HOST=ssh.amdumo.fun ws04 deploy voicy --yes --health-timeout 120
WS04_HOST=ssh.amdumo.fun ws04 stack status voicy
WS04_HOST=ssh.amdumo.fun ws04 audit --stack voicy --since 1h
```

The final check must confirm a healthy container with no restart, a sane
`/healthz` body (`db`, `telegram_initialized`, `telegram_polling_fresh`, no
stuck jobs), and no token or key in logs.
