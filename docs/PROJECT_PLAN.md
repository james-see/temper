# Project Plan

> **Not the source of truth.** `ROADMAP.md` is Temper's authoritative plan; this file is a thin pointer plus the durable thesis and scope notes. Phase → roadmap map:
>
> | Phase | Maps to | Status |
> |---|---|---|
> | Phase 0 — Foundation | v0.1 | done |
> | Phase 1 — Native supervised loop | v0.1 | done |
> | Phase 2 — Meta-harness | v0.2 | done (Aider later) |
> | Phase 3 — Arbiter | v0.5 core | done (policies, chains, accounting; classifier partial) |
> | Phase 4 — Reflex maturity | v0.4 | done (semantic stagnation, full compaction later) |
> | Phase 5 — Learning | v0.11 | later (telemetry schema only) |
>
> The original engineering backlog (event envelope, SQLite store, worktree manager, provider/agent interfaces, OpenAI/oMLX/Ollama providers, exec adapter, Reflex detectors, evaluator, Arbiter policies, run inspection) is fully built; see `ROADMAP.md` for per-item evidence.

## Product thesis

Temper is the open-source control plane for coding agents: route, supervise, recover and improve execution across Cursor, Claude Code, Codex, OpenCode, Hermes and other agents, using cloud or local models including oMLX and Ollama.

## Scope discipline

Do not initially build:

- an IDE
- autocomplete
- GitHub replacement
- project management
- a generic RAG platform
- a vector database
- a custom model
- a large hosted control plane

The wedge is **progress-aware supervision and recovery across agents**.
