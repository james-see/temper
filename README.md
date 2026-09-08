# Temper

**The open-source adaptive control plane for coding agents.**

Temper supervises coding agents and models: it measures whether they are making progress, detects stalls and loops, changes strategy, and verifies outcomes. It is not another monolithic coding agent. It sits **above** them.

```text
Goal → Plan → Action → Observation → Progress evaluator
                                      ├── progressing → continue
                                      ├── uncertain   → investigate
                                      ├── stalled     → replan
                                      ├── looping     → intervene
                                      ├── regressing  → rollback
                                      └── complete    → verify + stop
```

## Status

**v0.1** — native agent, providers, Reflex baseline, SQLite event store, operator TUI.

## Install

```bash
go install github.com/james-see/temper/cmd/temper@latest
```

## CLI

```bash
temper                         # TUI splash + goal input
temper run "fix the auth tests" # live TUI run
temper run --plain "..."        # CI / no alt screen
temper inspect <run-id>
temper config show
temper config path
temper version
```

On a TTY, `temper` and `temper run` open an OpenCode-style TUI (subscriber; runtime is source of truth). `--plain` logs events to stdout.

### Keys

`?` help · `s` status · `e` last progress/loop · `j`/`k` scroll · `g`/`G` top/bottom · `ctrl+c` cancel · `q` quit · `esc` close overlay

## Config

Later wins:

1. defaults
2. `$XDG_CONFIG_HOME/temper/temper.yaml` or `~/.config/temper/temper.yaml`, plus `~/.temper.yaml`
3. `./temper.yaml` or `./.temper/config.yaml`
4. `TEMPER_*`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`
5. `--plain --config --agent --provider --model`

See [`examples/temper.yaml`](examples/temper.yaml). Data dir: `.temper/` in the target repo (`temper.db`, `worktrees/`, `artifacts/`).

## Providers

OpenAI-compatible (OpenAI, oMLX `localhost:8000`), Ollama `/api/chat`, Anthropic Messages, Gemini `generateContent`.

v0.1 agent is **native** only (shell, read, write, patch, search, git). External agents are v0.2.

## Reflex judge

Heuristics are primary. A local SLM is invoked **only** when the assessment is `Uncertain` (or semantic-stagnation).

Default: [LiquidAI/LFM2.5-2.6B-GGUF](https://huggingface.co/LiquidAI/LFM2.5-2.6B-GGUF) `Q4_K_M` (~1.67GB) via Ollama, else llama.cpp OpenAI-compat. Missing model → heuristics only. Weights are not vendored.

LFM Open License v1.0 is **not OSI** (free commercial under $10M revenue). Temper itself is Apache-2.0.

```bash
# optional
llama-server -hf LiquidAI/LFM2.5-2.6B-GGUF:Q4_K_M
```

## Docs

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
- [`docs/REFLEX.md`](docs/REFLEX.md)
- [`docs/EVENT_MODEL.md`](docs/EVENT_MODEL.md)
- [`docs/ROADMAP.md`](docs/ROADMAP.md)

Site: [temper.baby](https://temper.baby)

## License

Apache-2.0.
