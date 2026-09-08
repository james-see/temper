# Agents and Providers

## Principle

**Agents are execution environments. Providers are inference backends.**

Do not couple them.

## Agent interface

Conceptual Go API:

```go
type Agent interface {
    ID() string
    Capabilities(ctx context.Context) (AgentCapabilities, error)
    Start(ctx context.Context, req TaskRequest) (Session, error)
    Resume(ctx context.Context, sessionID string) (Session, error)
    Interrupt(ctx context.Context, sessionID string) error
    Events(ctx context.Context, sessionID string) (<-chan Event, error)
}
```

Capabilities may include:
- streaming
- MCP
- ACP
- resume
- subagents
- worktrees
- structured output
- model override
- tool-call visibility
- native checkpoints

## Initial agent adapters

P0/P1 targets:
- `native`
- `opencode`
- `claude-code`
- `codex`
- `cursor`
- `hermes`
- generic `exec`

Later:
- Goose
- Aider
- community adapters

## Generic exec adapter

An unsupported agent should be configurable without a new Temper release.

```yaml
agents:
  my-agent:
    type: exec
    command:
      start: >
        myagent run
        --model {{ model }}
        --prompt {{ prompt }}
    output:
      format: jsonl
```

## Provider interface

```go
type Provider interface {
    ID() string
    Models(ctx context.Context) ([]Model, error)
    Capabilities(ctx context.Context, model Model) (ModelCapabilities, error)
    Generate(ctx context.Context, req Request) (<-chan ModelEvent, error)
}
```

Model capabilities:
- context window
- max output tokens
- tool use
- parallel tool use
- vision
- structured output
- reasoning controls
- embeddings
- prompt caching
- streaming
- local/remote
- observed tokens/sec
- cost metadata

## Initial provider adapters

P0:
- OpenAI
- Anthropic
- Gemini
- OpenAI-compatible
- **oMLX**
- **Ollama**

P1:
- OpenRouter
- Bedrock
- Groq
- Together
- xAI
- vLLM
- LM Studio

## Why oMLX is first-class

Even when it can be accessed through compatible HTTP APIs, a first-class adapter lets Temper understand local-server-specific telemetry, available models, cache behavior, latency, throughput and local-only policy.

## Why Ollama is first-class

Ollama remains a common local inference/runtime target and should work without forcing users through an OpenAI-compatibility shim.
