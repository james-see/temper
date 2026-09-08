# Roadmap

Temper's roadmap is intentionally layered: first prove **progress-aware supervision and recovery**, then expand into the surrounding control-plane features needed to run coding agents reliably across local, remote, interactive, and automated environments.

The goal is not to become an IDE. The goal is to become the runtime that can sit underneath a CLI, TUI, editor, desktop app, CI job, remote worker, or automation trigger.

## v0.1 — Runtime foundation

Core execution primitives:

- Go CLI
- configuration loader and schema
- append-only event store
- run state machine
- run inspection
- git workspace abstraction
- checkpoints and rollback
- native agent loop
- shell / read / write / patch / search / git tools
- test and compile evaluator interface
- structured logs and telemetry
- cancellation and timeouts
- artifact storage

Providers:

- oMLX
- Ollama
- OpenAI-compatible endpoints
- OpenAI
- Anthropic
- Gemini

Reflex baseline:

- repeated-action detector
- repeated-error fingerprint detector
- basic recovery ladder

**Exit criterion:** Temper can execute and inspect a coding task, detect a simple loop, intervene, and verify the outcome.

---

## v0.2 — Meta-harness and session continuity

First-class external agent adapters:

- OpenCode
- Claude Code
- Codex
- Cursor Agents
- Hermes Agent
- generic `exec` adapter
- Goose / Aider adapters as capacity allows

Agent runtime capabilities:

- capability negotiation
- model/provider override when supported
- start / stop / interrupt
- resumable sessions
- persistent conversation/session IDs
- agent status and lifecycle events
- agent hooks and notifications
- session recovery after Temper restart
- prompt queuing while an agent is active
- durable prompt drafts / pending input
- attachments and file references
- plan / build / debug / custom execution modes where adapters expose them
- subagent event capture
- clean handoff from one agent to another

**Exit criterion:** one task can start in one agent, survive a restart, and be recovered or continued by another agent without losing workspace or execution history.

---

## v0.3 — Workspace and git lifecycle

Workspace management:

- isolated git worktrees
- existing-workspace mode
- configurable worktree directories
- base-branch / branch-strategy policies
- task archive / restore / delete
- deterministic teardown and orphan-process cleanup
- disk-usage accounting and worktree cleanup
- workspace lifecycle scripts
- project-level environment variables
- project / task configuration layering
- file watching and repository invalidation
- content search across workspace files
- symlink/path handling
- empty-repository bootstrap

Git / forge lifecycle:

- branch creation and publishing
- fork workflows
- commit creation and history
- automatic pull-request detection
- create draft / regular pull requests
- synchronize PR state
- mark ready / close / merge
- base-branch resolution
- changed-file and commit views
- CI/check status ingestion
- review comments and line comments
- per-commit review data
- multiple forge identities/accounts
- enterprise/self-hosted forge support

**Exit criterion:** a Temper task has a complete, recoverable lifecycle from workspace creation through verified pull request.

---

## v0.4 — Reflex: progress intelligence

Advanced observable-progress detection:

- cyclic tool/action sequences
- repeated error fingerprints
- repository-state stagnation
- test-score stagnation
- compiler/linter stagnation
- regression detection
- token/cost burn without progress
- planning-without-execution detection
- file edit/revert oscillation
- semantic stagnation
- configurable per-project detectors
- detector confidence and evidence trails

Recovery actions:

- goal re-anchoring
- replan
- context compaction / failed-attempt summary
- critic/debugger invocation
- model switch
- provider switch
- agent switch
- checkpoint rollback
- alternate-approach fork
- human escalation
- intervention attempt limits / backoff

**Exit criterion:** benchmark suite demonstrates a measurable completion-rate improvement with Reflex enabled versus disabled.

---

## v0.5 — Arbiter: adaptive routing

Routing inputs:

- task/repository classification
- language/framework detection
- agent capabilities
- provider/model capabilities
- context requirements
- privacy constraints
- cost budgets
- latency targets
- provider health
- local resource availability
- previous attempts
- historical success

Routing features:

- deterministic policy rules
- hard allow/deny constraints
- local-first execution
- fallback and escalation chains
- planner / implementer / reviewer role assignment
- model effort/reasoning controls
- cost/token/latency accounting
- candidate scoring and explainable routing rationale
- provider health checks
- model catalog and capability discovery

**Exit criterion:** Temper can select among local and hosted model/agent combinations, explain the selection, and automatically escalate when the initial strategy stalls.

---

## v0.6 — Extensibility, tools, skills, and policy

Protocols and extension points:

- MCP local servers
- MCP remote servers
- MCP authentication/OAuth
- per-agent MCP enablement
- ACP support where useful
- custom tools
- reusable skills
- reusable agent profiles / roles
- reusable rules/instructions
- reusable commands/prompts
- plugin/hook API
- lifecycle hooks
- compaction hooks
- event subscribers
- extension discovery/catalog
- versioned extension manifests

Permissions and policy:

- allow / ask / deny rules
- path-scoped file permissions
- command-pattern shell permissions
- external-directory controls
- network controls
- provider/model allow/deny policy
- per-agent permissions
- per-tool permissions
- explicit auto-approval mode
- secrets/environment filtering
- secret redaction in events/logs
- human approval boundaries

**Exit criterion:** a third party can add an agent, provider, tool, skill, hook, or policy without modifying Temper core.

---

## v0.7 — Remote and multi-machine execution

Remote workspace support:

- SSH workers
- Tailscale-friendly SSH support
- remote workspace server/daemon
- local and remote workspaces through the same API
- reconnect after sleep/network interruption
- resumable remote agent sessions
- remote file search / file watching
- remote terminals
- port forwarding for dev servers
- remote preview forwarding
- per-machine environment configuration
- per-machine available agents/providers/models
- per-machine MCP/skills inventory
- CPU/RAM/GPU/disk/resource telemetry
- remote process supervision and cleanup

Execution environments:

- container/sandbox runner
- isolated VM/cloud runner interface
- environment build/bootstrap scripts
- cached/warm development environments
- last-known-good environment snapshots
- dependency/bootstrap caching
- local / SSH / container / cloud execution targets
- Linux/macOS/Windows path and shell abstraction
- ARM64/x64 support

Proof artifacts:

- command/test logs
- screenshots
- browser artifacts
- videos where supported
- structured completion evidence

**Exit criterion:** a run can move between local and remote execution targets while preserving the same Temper task/event model.

---

## v0.8 — Automations and external work sources

Automations:

- scheduled recurring runs
- event-triggered runs
- run history
- rerun
- templates
- configurable branch/base strategy
- optional auto-approval policy
- concurrency controls
- budgets per automation
- convert automation run into interactive task
- notifications on success/failure/intervention

Work-source integrations:

- GitHub Issues / Pull Requests
- GitLab issues / merge requests
- Linear
- Jira
- generic webhook ingestion
- generic issue/task adapter interface
- context templates per integration/project
- branch naming derived from external work items
- attachments and remote documents as task context
- pluggable additional trackers/document systems

**Exit criterion:** Temper can accept a task from an external system, schedule or trigger execution, create an isolated workspace, run an agent, verify it, and publish the resulting change.

---

## v0.9 — Operator control surface

The control surface is for **observing and steering the runtime**, not replacing a developer's editor.

CLI/TUI:

- project/task/session browser
- multiple conversations per task
- agent/model/mode switchers
- run timeline
- Reflex intervention display
- Arbiter decision display
- prompt queue
- command palette
- prompt library
- reusable task templates
- notifications
- resource monitor
- searchable logs

Code/workspace inspection:

- file tree
- content search
- git status
- diff viewer
- commit list
- PR/check summaries
- terminal sessions
- persistent terminal scrollback
- side-by-side/split views where useful

Preview/testing:

- dev-server detection
- local/remote port previews
- browser preview surface
- screenshot capture
- authenticated preview profiles
- UI annotation/target feedback as a later enhancement

Optional clients:

- local web UI
- Tauri + Svelte desktop client
- editor extensions through protocol/API integration

**Exit criterion:** an operator can understand what every active agent is doing, inspect evidence, steer execution, and intervene without reading raw logs.

---

## v0.10 — Evaluation, parallelism, and tournaments

- parallel worktrees
- parallel sub-tasks
- workflow DAGs
- planner / implementer / reviewer workflows
- multiple candidate solutions
- automated test/evaluator execution
- LLM critic/judge as supplemental evidence
- diff complexity scoring
- candidate comparison
- winning-solution selection
- merge/rebase of parallel work
- comparative agent/model analytics
- benchmark packs
- replay against historical tasks

**Exit criterion:** Temper can execute multiple strategies for the same task and select a verified winner using explicit evaluation criteria.

---

## v0.11 — Learn

- policy version history
- run/outcome dataset
- strategy performance statistics
- model/agent success priors
- cost-normalized success metrics
- local-vs-cloud escalation learning
- Reflex threshold optimization
- recovery-order optimization
- workflow-combination scoring
- offline historical replay
- held-out benchmark evaluation
- candidate policy generation
- automatic recommendations
- canary/opt-in policy rollout
- rollback of policy versions
- safe policy promotion
- exploration budget / contextual-bandit experiments when data justifies them

**Exit criterion:** historical data produces a policy change that improves held-out task performance without violating cost, privacy, or permission constraints.

---

## v1.0 — Reliable agent control plane

Stability and team-readiness:

- stable public plugin/adapter APIs
- migration/versioning strategy
- robust restart/crash recovery
- distributed run locking
- idempotent side effects
- budgets and quotas
- audit trail
- RBAC
- shared organization policies
- team agent/provider catalogs
- shared skills/rules/templates
- organization-wide routing policy
- organization-level telemetry
- cross-repository learning with privacy controls
- hosted control-plane option
- self-hosted server deployment
- enterprise SSO/secrets integration

## Cross-cutting requirements

These apply throughout the roadmap rather than belonging to one release:

- resumability over restart/reconnect
- clear capability discovery rather than hard-coded assumptions
- local-first support
- observable state transitions
- event-backed auditability
- safe cancellation and process cleanup
- cross-platform path/process handling
- configuration migration
- resource accounting
- explicit permissions
- human override at every autonomous layer
- no dependence on hidden model reasoning for progress detection

## Product boundary

Temper should **integrate with** editors, terminals, browsers, issue trackers, forges, model providers and coding agents rather than attempting to replace all of them.

The differentiating center remains:

> **Observe execution, determine whether useful progress is happening, choose the best available strategy, recover when it is not, and improve those decisions from evidence over time.**
