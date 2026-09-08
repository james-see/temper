# Architecture

## Design goals

1. Supervise any coding agent without requiring it to be rewritten for Temper.
2. Treat model providers and coding agents as separate abstractions.
3. Make observable progress measurable.
4. Make every execution replayable and inspectable.
5. Keep local models first-class.
6. Allow routing and recovery policies to evolve independently from agents.
7. Prefer deterministic infrastructure around nondeterministic models.

## High-level system

```text
CLI / TUI / API
      │
      ▼
Temper Runtime
      │
      ├── Run Manager
      ├── Workflow Engine
      ├── Event Store
      ├── Workspace Manager
      ├── Evaluators
      ├── Arbiter
      └── Reflex
              │
              ▼
        Agent Adapters
              │
     ┌────────┼─────────┐
     ▼        ▼         ▼
  Cursor    Codex    OpenCode ... Native
              │
              ▼
       Provider Adapters
              │
 Anthropic/OpenAI/Gemini/oMLX/Ollama/...
```

## Core boundaries

### Runtime
Owns run lifecycle, durable state, budgets, events, workflows, checkpoints and cancellation.

### Workspace manager
Creates isolated git worktrees, snapshots state, computes diffs, restores checkpoints, and exposes repository-state signals to Reflex.

### Agent adapters
Wrap execution environments such as Cursor Agents, Claude Code, Codex, OpenCode and Hermes.

### Provider adapters
Wrap inference backends such as Anthropic, OpenAI, oMLX and Ollama. Provider capabilities are negotiated rather than reduced to a lowest-common-denominator API.

### Evaluator
Produces evidence: tests, compile state, lint, acceptance criteria, diff quality, task-specific checks and optional model-based judgments.

### Arbiter
Chooses agents/models/strategies based on task features, constraints, capabilities, history and policy.

### Reflex
Determines whether execution is progressing, stalled, looping or regressing and selects recovery actions.

## Execution model

A run is a durable state machine backed by an append-only event stream.

```text
created → classified → planned → executing
                              ↘ evaluating
                              ↘ recovering
                              ↘ waiting_human
                              ↘ completed
                              ↘ failed
                              ↘ cancelled
```

A single task is not permanently bound to a single agent. A run may hand work from OpenCode to Codex, or Cursor to Hermes, while preserving workspace and run history.

## Workflow model

Workflows should eventually be DAGs:

```text
              plan
               │
        ┌──────┴──────┐
        ▼             ▼
    backend        frontend
      Codex          Cursor
        └──────┬──────┘
               ▼
             review
         OpenCode + oMLX
               │
               ▼
             repair
             Hermes
```

## Storage

Initial local implementation:
- SQLite for run/event metadata
- git/worktrees for repository state
- content-addressed artifacts on disk
- optional OpenTelemetry export

The event schema should remain portable enough for a hosted PostgreSQL implementation later.

## Security

Agent execution can run arbitrary code. Temper should treat workspace execution as hostile by default.

Planned controls:
- explicit workspace roots
- configurable shell policy
- environment allow/deny lists
- secret redaction
- optional container/sandbox execution
- per-agent permissions
- network policy hooks
- audit events for external actions
