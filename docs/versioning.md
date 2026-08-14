# Versioning

Develop on `dev`. Publish tags from `main`.

First line: `v0.1.0-alpha.1`, `v0.1.0-beta.1`, `v0.1.0-rc.1`, then `v0.1.0`.

Patch for fixes. Minor for compatible product or operations changes. Reserve `v1.0.0` for a stable production contract.

Breaking-sensitive before 1.0: required env, schema, Deepgram/Telegram contracts, group `/v` `/vp` semantics, `file_id` cache.
