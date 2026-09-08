# Temper

**The open-source control plane for coding agents.**

Temper is an adaptive meta-harness for AI software engineering. It supervises coding agents and models, measures whether they are actually making progress, detects stalls and loops, changes strategy when needed, and learns from previous runs which agents, models, tools, and workflows work best.

Temper is not another monolithic coding agent. It sits **above** them.

```text
                              TEMPER
                 Adaptive Meta-Harness / Control Plane
                                  │
                    ┌─────────────┴─────────────┐
                    │                           │
               Supervisor                  Evaluator
          progress / stagnation        tests / compiler / lint
          loop / regression            diff / acceptance criteria
          recovery / routing           cost / latency / quality
                    │                           │
                    └─────────────┬─────────────┘
                                  │
                              ARBITER
                       routing / policy engine
                                  │
             ┌────────────────────┴────────────────────┐
             │                                         │
         AGENT LAYER                              NATIVE AGENT
             │                                         │
 Cursor · Claude Code · Codex · OpenCode · Hermes · Goose · Aider
             │                                         │
             └────────────────────┬────────────────────┘
                                  │
                             MODEL LAYER
                                  │
 Anthropic · OpenAI · Gemini · OpenRouter · Bedrock · Groq · xAI · Together
                       oMLX · Ollama · vLLM · LM Studio
                     + any OpenAI-compatible provider
```

## Why Temper?

Modern coding agents still fail in predictable ways:

- repeat the same tool calls or edits
- cycle between approaches without converging
- burn tokens while repository state barely changes
- repeatedly hit the same compiler/test error
- regress working behavior while trying to fix something else
- keep using a weak model or agent when another would be better
- declare success without satisfying acceptance criteria

Temper makes **observable progress** a first-class runtime concept.

```text
Goal → Plan → Action → Observation → Progress evaluator
                                      ├── progressing → continue
                                      ├── uncertain   → investigate
                                      ├── stalled     → replan
                                      ├── looping     → intervene
                                      ├── regressing  → rollback
                                      └── complete    → verify + stop
```

Temper does not need access to a model's hidden reasoning. It evaluates external state: git diffs, tests, compiler errors, tool-call patterns, file activity, acceptance criteria, cost, and other measurable signals.

## Core concepts

### Temper
The runtime and control plane. It owns runs, workflows, event history, workspaces, checkpoints, budgets, evaluation, and policy execution.

### Arbiter
The routing and policy engine. Arbiter decides **who should do the work** and can change that decision mid-run.

### Reflex
The intervention and recovery engine. Reflex answers: **is this run still making progress, and if not, what should happen next?**

Initial detectors include exact repetition, cyclic action sequences, repeated error fingerprints, repository stagnation, regressions, token burn, planning-without-execution loops, and later semantic stagnation.

Recovery actions can include replan, critic/debugger, model switch, agent switch, rollback, fork, or human escalation.

## Agents are not models

Temper deliberately separates **agent runtimes** from **model providers**.

Agents:
- Cursor Agents
- Claude Code
- OpenAI Codex
- OpenCode
- Hermes Agent
- Goose
- Aider
- Temper native agent
- arbitrary external agents through a generic `exec` adapter

Providers:
- Anthropic
- OpenAI
- Google Gemini
- OpenRouter
- AWS Bedrock
- Groq
- Together
- xAI
- **oMLX**
- **Ollama**
- vLLM
- LM Studio
- OpenAI-compatible endpoints

This allows combinations such as:

```text
OpenCode → oMLX → local coder model
Hermes → OpenRouter → hosted model
Temper Native → Ollama → local model
Claude Code → Anthropic
Cursor Agent → configured Cursor model
```

## Local-first is first-class

```text
simple task
   ↓
OpenCode + local model via oMLX
   ↓
progress stalls
   ↓
Reflex intervention
   ↓
Arbiter escalates to Codex / Claude Code / Cursor
   ↓
local model performs final review
```

Support for oMLX and Ollama is part of the core design, not an afterthought.

## Self-improving, without hand-waving

Temper's initial meaning of "self-improving" is **empirical policy optimization**, not uncontrolled self-modifying code.

Every run produces structured evidence about task classification, agent/model selection, policy versions, tool use, interventions, evaluator results, cost, latency, and final outcome. Historical runs can then improve routing and recovery policies.

## Event-sourced execution

Example events:

```text
run.started
task.classified
plan.created
agent.started
model.called
tool.requested
tool.completed
file.modified
test.executed
progress.evaluated
loop.detected
strategy.changed
checkpoint.created
checkpoint.restored
run.completed
```

This enables replay, inspection, forking, comparison, offline evaluation, and policy optimization.

Planned CLI:

```bash
temper run "fix the failing authentication tests"
temper inspect <run-id>
temper replay <run-id>
temper fork <run-id> --at step:42 --agent codex
temper eval ./benchmarks/go-bugs
temper learn
```

## Initial stack

| Concern | Initial choice |
|---|---|
| Runtime | Go |
| CLI | Cobra |
| TUI | Bubble Tea |
| Local state | SQLite |
| Config | YAML |
| Workspace isolation | Git worktrees |
| Tool ecosystem | MCP |
| Editor/agent connectivity | ACP where useful |
| Telemetry | OpenTelemetry |
| Local API | HTTP/WebSocket |
| Desktop UI later | Tauri + Svelte |

## Docs

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- [`docs/AGENTS_AND_PROVIDERS.md`](docs/AGENTS_AND_PROVIDERS.md)
- [`docs/ARBITER.md`](docs/ARBITER.md)
- [`docs/REFLEX.md`](docs/REFLEX.md)
- [`docs/EVENT_MODEL.md`](docs/EVENT_MODEL.md)
- [`docs/SELF_IMPROVEMENT.md`](docs/SELF_IMPROVEMENT.md)
- [`docs/PROJECT_PLAN.md`](docs/PROJECT_PLAN.md)
- [`docs/ROADMAP.md`](docs/ROADMAP.md)
- [`docs/SWOT.md`](docs/SWOT.md)

## MVP thesis

> Temper can recognize that a coding agent is no longer making useful progress, intervene intelligently, and measurably improve the probability of task completion.

## What Temper is not

Initially, Temper is not an IDE, code editor, autocomplete engine, GitHub replacement, project-management suite, generic RAG framework, vector database, or custom foundation model.

Temper is the **runtime + supervisor + evaluator + optimizer**.

## Status

**Pre-alpha / architecture phase.**

## License

Apache-2.0.