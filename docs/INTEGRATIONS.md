# Integrations

Temper separates **agent runtimes** from **model providers** so either side can evolve independently.

## Agent adapters

| Agent | Priority | Notes |
|---|---:|---|
| OpenCode | P0 | Key meta-harness target; local/cloud model flexibility |
| Claude Code | P0 | First-class external coding-agent adapter |
| OpenAI Codex | P0 | First-class external coding-agent adapter |
| Cursor Agents | P0 | First-class external coding-agent adapter |
| Hermes Agent | P0 | Strong fit for delegation, research, and subagent workflows |
| Temper Native | P0 | Minimal native agent loop for direct provider execution |
| Generic `exec` | P0 | Allows unsupported agent CLIs to be configured without a Temper release |
| Goose | P1 | Community/first-party adapter candidate |
| Aider | P1 | Community/first-party adapter candidate |

## Provider adapters

| Provider | Priority | Notes |
|---|---:|---|
| oMLX | P0 | First-class local inference target with capability/telemetry awareness |
| Ollama | P0 | First-class local inference target |
| OpenAI | P0 | Native hosted adapter |
| Anthropic | P0 | Native hosted adapter |
| Gemini | P0 | Native hosted adapter |
| OpenAI-compatible | P0 | Generic compatibility layer |
| OpenRouter | P1 | Broad hosted model routing |
| Bedrock | P1 | Enterprise/provider abstraction |
| Groq | P1 | Low-latency hosted inference |
| Together | P1 | Hosted open-model inference |
| xAI | P1 | Hosted provider |
| vLLM | P1 | Self-hosted inference |
| LM Studio | P1 | Local inference |

## Design rule

An agent may use a provider internally, but Temper must not assume a permanent binding between the two. Arbiter should be able to reason about agent capabilities, provider capabilities, policy constraints, cost, privacy, and historical success independently whenever the underlying agent supports model/provider selection.
