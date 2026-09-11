# Temper Agent Guide

This file provides guidance for LLMs and human contributors working on the Temper repository.

## Project Overview

Temper is an open-source adaptive control plane for coding agents. It supervises agents (like Cursor, Claude Code, Codex, OpenCode) by measuring progress, detecting stalls/loops, changing strategy, and verifying outcomes. It sits above the agents, not as another monolithic agent.

Key components:
- **Agent**: Native Go agent with shell, read, write, patch, search, git tools.
- **Reflex Judge**: Heuristics-based progress evaluator; invokes a local SLM (default: oamazonasgabriel/lfm2.5-2.6b:q4_k_m-8gbGPU) only when uncertain.
- **Event Model**: SQLite-backed event store for run history.
- **TUI**: Charmbracelet-based terminal interface (alt screen) with `--plain` mode for CI.
- **CLI**: `temper` (TUI), `temper run` (supervised run), `temper inspect`, `temper config`, etc.

## Development Setup

### Prerequisites
- Go 1.25.0+
- Optional: Ollama (for reflex judge), or configure other providers via API keys.

### Building
```bash
go build -o temper ./cmd/temper
```

### Running Tests
```bash
go test ./...
```

### Running the TUI
```bash
temper
# or
temper run "your goal here"
```

### Configuring Providers
Temper probes providers in this order:
1. Ollama Cloud (if `OLLAMA_API_KEY` set)
2. Local Ollama (`:11434`)
3. OpenAI-compatible (if `OPENAI_API_KEY` set)
4. Anthropic (if `ANTHROPIC_API_KEY` set)
5. Gemini (if `GEMINI_API_KEY` set)

Config files (later wins):
- Defaults
- `$XDG_CONFIG_HOME/temper/temper.yaml` or `~/.config/temper/temper.yaml`
- `~/.temper.yaml`
- `./temper.yaml` or `./.temper/config.yaml`
- Environment variables (`TEMPER_*`, provider API keys)
- CLI flags (`--plain --config --agent --provider --model`)

See `examples/temper.yaml` for an example.

## Coding Conventions

### Go
- Follow [Effective Go](https://golang.org/doc/effective_go.html) and Go [code review comments](https://github.com/golang/go/wiki/CodeReviewComments).
- Use `golangci-lint` for linting (config in `.golangci.yml` if present).
- Tests: table-driven where appropriate; keep test files next to implementation (`*_test.go`).
- Error handling: wrap errors with `%w` or use `errors.Is`/`errors.As` for sentinel errors.
- Logging: use `slog` (standard library) with structured attributes.

### Naming
- Packages: lowercase, single word, no underscores.
- Interfaces: suffix `-er` when appropriate (e.g., `Provider`, `Store`).
- Structs: `PascalCase`.
- Methods: `camelCase`.

### Imports
- Group: standard library, external, internal.
- Use `goimports` format.

### Documentation
- Exporting functions/types: must have godoc comments.
- Package comments: file-level comment at top of `doc.go` or one `.go` file per package.

## Testing
- Unit tests: `go test ./...`.
- Integration tests: none currently; avoid heavy external dependencies in unit tests.
- Mocking: use interfaces for dependency injection; consider `gomock` or manual fakes if needed.

## Releasing
- The project uses Git tags for releases (e.g., `v0.1.0`).
- Release process:
  1. Ensure `main` is up to date and tests pass.
  2. Update version in `cmd/temper/main.go` if applicable (currently version is static? check).
  3. Create tag: `git tag vX.Y.Z && git push origin vX.Y.Z`.
  4. GitHub Actions will build and publish binaries (if configured).
- Homebrew formula: maintained in `james-see/tap` tap.

## LLMs and Agents
Temper is designed to supervise coding agents. When extending Temper:
- Consider the reflex judge loop: Goal → Plan → Action → Observation → Progress evaluator → (progressing|uncertain|stalled|looping|regressing|complete).
- The reflex judge is pluggable; defaults to heuristics + optional SLM.
- Tool implementations live in `internal/agent/tools/` (shell, read, write, patch, search, git).
- External agents use the Sidecar interface (Poll + Inject). Wired: Hermes, Cursor, OpenCode. Claude Code and Codex are next.

## Directory Structure
```
.
├── cmd/                    # Main applications (temper CLI)
├── dist/                   # Built binaries (if checked in)
├── docs/                   # Markdown documentation (ARCHITECTURE.md, REFLEX.md, etc.)
├── examples/               # Example config files
├── internal/
│   ├── agent/              # Agent core, tools, known agents
│   ├── arbiter/            # Decision making based on reflex assessments
│   ├── artifact/           # Artifact storage (files, outputs)
│   ├── config/             # Configuration loading and persistence
│   ├── debuglog/           # Structured logging
│   ├── evaluator/          # Reflex judge implementation
│   ├── event/              # Event model and storage
│   ├── provider/           # LLM provider abstraction
│   ├── run/                # Run orchestration (goal, plan, action, observation)
│   ├── store/              # SQLite event store
│   ├── term/               # Terminal abstraction
│   ├── tui/                # Terminal UI (Charmbracelet)
│   └── workspace/          # Worktree and workspace management
├── site/                   # Astro-based website (temper.baby)
└── go.mod, go.sum
```

## Getting Help
- Check `docs/` for detailed design documents.
- File issues on GitHub for bugs or feature requests.
- For contribution questions, open a discussion or contact maintainers.
