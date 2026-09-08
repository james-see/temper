# Project Plan

## Product thesis

Temper is the open-source control plane for coding agents: route, supervise, recover and improve execution across Cursor, Claude Code, Codex, OpenCode, Hermes and other agents, using cloud or local models including oMLX and Ollama.

## Phase 0 — Foundation

Deliver:
- Go module and CLI shell
- configuration loader
- event envelope + SQLite store
- run state machine
- git workspace/checkpoint manager
- structured logging
- provider and agent interfaces

Exit criterion:
- `temper run` can create a run, workspace, event stream and invoke a stub agent.

## Phase 1 — Native supervised loop

Deliver:
- basic native agent
- shell/read/write/patch/search/git tools
- OpenAI-compatible provider
- Anthropic provider
- oMLX adapter
- Ollama adapter
- basic test/compile evaluator
- first Reflex detectors

Exit criterion:
- Temper can execute a coding task and detect repeated action/error loops.

## Phase 2 — Meta-harness

Deliver:
- OpenCode adapter
- Claude Code adapter
- Codex adapter
- Cursor adapter
- Hermes adapter
- generic exec adapter
- handoff between agents
- persistent session metadata

Exit criterion:
- one task can start in one agent and be recovered by another without losing workspace state.

## Phase 3 — Arbiter

Deliver:
- task/repository classifier
- capability negotiation
- deterministic routing policies
- cost and latency accounting
- local-first rules
- fallback/escalation chains

Exit criterion:
- Temper can choose among multiple agents/providers and explain why.

## Phase 4 — Reflex maturity

Deliver:
- cyclic action detection
- error fingerprinting
- repository stagnation
- regression detection
- token-burn detector
- recovery ladder
- fork/rollback recovery

Exit criterion:
- benchmark demonstrates a higher completion rate with Reflex enabled than disabled.

## Phase 5 — Learning

Deliver:
- run analytics
- policy versioning
- benchmark replay
- strategy comparison
- policy recommendations
- `temper learn`

Exit criterion:
- historical data produces a policy change that improves held-out benchmark results.

## Initial engineering backlog

1. Define event envelope and run state machine
2. Implement SQLite event store
3. Implement workspace/worktree manager
4. Define provider capability interface
5. Define agent capability interface
6. Implement OpenAI-compatible provider
7. Implement oMLX provider
8. Implement Ollama provider
9. Implement generic exec agent adapter
10. Implement Reflex repeated-error detector
11. Implement Reflex action-cycle detector
12. Implement evaluator interface
13. Implement test-command evaluator
14. Implement Arbiter static policy engine
15. Implement run inspection CLI

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
