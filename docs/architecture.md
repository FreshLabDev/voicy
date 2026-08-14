# Architecture

One Go process: Telegram long poll, Deepgram streaming (WS) with prererecorded HTTP fallback, PostgreSQL for cache/jobs/stats, a small `/healthz` server.

```text
update
  → decide (pure)
  → core.touch
  → cache lookup by file_id
       hit  → format → send
       miss → download → Deepgram stream (draft) → fallback POST
            → persist transcript
            → send final
            → stats if non-empty success
```

Identity lives in `core.*`. Product rows live in `voicetotext.*`.
