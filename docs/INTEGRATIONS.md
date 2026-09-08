# Integrations

Temper separates **agent runtimes**, **model providers**, **tools/protocols**, **work sources**, and **execution targets** so each layer can evolve independently.

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

Agent adapters should expose capability metadata where available:

- start/stop/interrupt
- session resume
- model override
- plan/build/debug modes
- MCP support
- ACP support
- subagents
- tool-call/event visibility
- permissions/auto-approval
- lifecycle hooks
- notifications
- worktree support

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

Provider capability discovery should include:

- context/output limits
- tool use / parallel tool use
- structured output
- vision
- reasoning/effort controls
- embeddings where applicable
- streaming
- prompt/context caching
- latency / observed throughput
- cost metadata
- local vs remote classification
- health/availability

## Tool and extension protocols

### MCP

Temper should support:

- local stdio servers
- remote HTTP transports
- authenticated/OAuth servers
- global and per-agent enablement
- permission rules per MCP namespace/tool
- MCP catalogs without requiring a central marketplace

### ACP

Use ACP where it provides clean conversation/session interoperability with external coding agents and clients. Temper's internal runtime should not depend exclusively on ACP.

### Native extensions

Temper should expose stable extension points for:

- custom tools
- skills
- rules/instructions
- commands/prompt templates
- agent profiles
- event hooks
- lifecycle hooks
- compaction hooks
- evaluators
- Reflex detectors
- recovery actions
- Arbiter scoring/routing plugins

## Source-control and forge integrations

The git core should remain forge-independent.

Initial forge integration targets:

- GitHub
- GitHub Enterprise
- GitLab / self-hosted GitLab
- generic git remotes

Capabilities:

- multiple identities/accounts
- branch publishing
- fork workflows
- pull/merge request creation
- draft/ready state
- PR/MR auto-detection
- CI/check ingestion
- review comments / line comments
- merge operations
- commit and changed-file metadata

## Work-source integrations

Temper tasks should be creatable from external work systems through a generic `WorkSource` interface.

Initial targets:

- GitHub Issues
- GitLab Issues
- Linear
- Jira
- generic webhooks

Later/community targets can include additional trackers and document systems without requiring changes to Temper core.

A work-source adapter can provide:

- title/description
- attachments
- acceptance criteria
- comments/history
- labels/priority
- linked branch/PR
- branch-name suggestion
- project-specific context template

## Execution targets

Temper's run model should be independent of where execution occurs.

Planned targets:

- local process
- local sandbox/container
- SSH host
- Tailscale-reachable SSH host
- remote Temper worker
- isolated VM/cloud worker

Execution targets expose:

- OS/architecture
- workspace roots
- available agents
- available providers/models
- installed skills/MCP servers
- environment/dependency capabilities
- CPU/RAM/GPU/disk telemetry
- port-forwarding ability
- sandbox/network policy capabilities

## Dev-server and preview integrations

Preview support exists to **verify agent output**, not to make Temper a browser product.

Planned capabilities:

- detect local dev servers
- capture ports from lifecycle scripts/processes
- SSH/remote port forwarding
- preview URLs
- screenshots as evaluator/proof artifacts
- authenticated preview profiles
- optional browser automation/tool adapters

## Automation triggers

Temper should expose the same run path for interactive and unattended execution.

Triggers can include:

- schedules
- webhooks
- issue assignment/label changes
- repository events
- CI events
- API calls
- manual CLI/TUI actions

Automation runs retain the same event stream, budgets, permissions, Reflex supervision, Arbiter routing, workspaces, artifacts, and evaluation as interactive tasks.

## Design rules

1. An agent may use a provider internally, but Temper must not assume a permanent binding between the two.
2. Generic protocols/adapters should handle the long tail; first-class integrations should add richer capability discovery and telemetry rather than creating architectural forks.
3. Temper should prefer capability negotiation over version/name-specific assumptions.
4. Integration failures must be observable events and should not corrupt run/workspace state.
5. Every integration that can perform side effects must participate in Temper's permission/audit model.
