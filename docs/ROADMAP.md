# Roadmap

> **Source of truth.** This file is Temper's authoritative plan. `PROJECT_PLAN.md` is a thin pointer into it (see that file). Statuses: `[done]` shipped, `[partial]` substantively started, `[next]` queued near-term follow-up, `[later]` not started. Evidence in parentheses.

Temper's roadmap is intentionally layered: first prove **progress-aware supervision and recovery**, then expand into the surrounding control-plane features needed to run coding agents reliably across local, remote, interactive, and automated environments.

The goal is not to become an IDE. The goal is to become the runtime that can sit underneath a CLI, TUI, editor, desktop app, CI job, remote worker, or automation trigger.

---

## v0.1 — Runtime foundation [done]

Core execution primitives:

- Go CLI [done] (`cmd/temper`)
- configuration loader and schema [done] (`internal/config`)
- append-only event store [done] (`internal/store`, SQLite)
- run state machine [done] (`internal/run` transitions)
- run inspection [done] (`temper inspect <run-id>`)
- git workspace abstraction [done] (`internal/workspace`)
- checkpoints and rollback [done] (`Checkpoint`/`Rollback`)
- native agent loop [done] (`drive`)
- shell / read / write / patch / search / git tools [done] (`internal/agent/tools.go`)
- test and compile evaluator interface [done] (`internal/evaluator`)
- structured logs and telemetry [done] (`internal/debuglog`, events)
- cancellation and timeouts [done]
- artifact storage [done] (`internal/artifact`)

Providers:

- oMLX [done] (`omlx` registry type)
- Ollama [done]
- OpenAI-compatible endpoints [done] (`openai` type + URL)
- OpenAI [done]
- Anthropic [done]
- Gemini [done]

Reflex baseline:

- repeated-action detector [done]
- repeated-error fingerprint detector [done]
- basic recovery ladder [done]

**Exit criterion:** Temper can execute and inspect a coding task, detect a simple loop, intervene, and verify the outcome. [done] (reflex uplift benchmark: 6/6 vs 0/6, VAL-2608)

---

## v0.2 — Meta-harness and session continuity [done]

First-class external agent adapters:

- OpenCode [done] (`internal/agent/opencode.go`)
- Claude Code [done] (`claude_code.go`)
- Codex [done] (`codex.go`)
- Cursor Agents [done] (`cursor.go`)
- Hermes Agent [done] (`hermes.go`)
- Muse Code [done] (`muse.go`)
- Goose [done] (`goose.go`)
- generic `exec` adapter [done] (`exec.go`)
- Aider adapters [later] (P1 candidate, no code)

Agent runtime capabilities:

- capability negotiation [partial] (`Capabilities` advertised and honored; no handshake protocol)
- model/provider override when supported [done] (`ModelOverride`)
- start / stop / interrupt [done] (`Start`/`Interrupt`; stop via interrupt + cancel)
- resumable sessions [done] (`Resume`/`Bind`)
- persistent conversation/session IDs [done]
- agent status and lifecycle events [done] (`AgentStarted`/`SidecarBound`/`AgentSwitched`)
- session recovery after Temper restart [done] (`temper continue <run-id>`)
- prompt queuing while an agent is active [done] (follow-up input)
- durable prompt drafts / pending input [next] (queue events exist; drafts UI missing)
- attachments and file references [later] (no code)
- plan / build / debug / custom execution modes [later] (no code)
- subagent event capture [later] (capability flag only, not driven)
- clean handoff from one agent to another [done] (`switch_agent` recovery rung)

**Exit criterion:** one task can start in one agent, survive a restart, and be recovered or continued by another agent without losing workspace or execution history. [done]

---

## v0.3 — Workspace and git lifecycle [partial]

Workspace management:

- isolated git worktrees [done] (`PrepareBranch`)
- existing-workspace mode [done] (`Attach`)
- configurable worktree directories [later] (fixed at `.temper/worktrees`)
- base-branch / branch-strategy policies [done] (`branch_prefix`, `base_branch`)
- task archive / restore / delete [done] (v0.4 lifecycle commands)
- deterministic teardown and orphan-process cleanup [partial] (worktree removal yes; orphan processes no)
- disk-usage accounting and worktree cleanup [done] (`DiskUsage`/`Orphans`)
- workspace lifecycle scripts [later] (no code)
- project-level environment variables [later] (no code)
- project / task configuration layering [done] (files + env + flags)
- file watching and repository invalidation [later] (no code)
- content search across workspace files [done] (search tool)
- symlink/path handling [done]
- empty-repository bootstrap [later] (worktree setup assumes existing history)

Git / forge lifecycle (`internal/forge`, GitHub via `gh` only):

- branch creation and publishing [done] (`Publish`)
- fork workflows [later] (attempt-branches are not repo forks)
- commit creation and history [done] (checkpoints; history via git)
- automatic pull-request detection [done] (`Forge.Find`)
- create draft / regular pull requests [done] (`CreateOptions.Draft`)
- synchronize PR state [done] (`Forge.Sync`)
- mark ready / close / merge [next] (no `Forge` methods yet)
- base-branch resolution [done] (`ResolveBase`)
- changed-file and commit views [later] (no code)
- CI/check status ingestion [done] (`Forge.Checks`)
- review comments and line comments [next] (no code)
- per-commit review data [later] (no code)
- multiple forge identities/accounts [later] (single `gh` auth)
- enterprise/self-hosted forge support [partial] (via `gh` host config, untested)

**Exit criterion:** a Temper task has a complete, recoverable lifecycle from workspace creation through verified pull request. [partial] (workspace side done; verified-PR loop needs merge ops)

---

## v0.4 — Reflex: progress intelligence [done]

Advanced observable-progress detection:

- cyclic tool/action sequences [done] (`repeated-cycle`)
- repeated error fingerprints [done] (`repeated-error`)
- repository-state stagnation [done] (`repo-stagnation`, VAL-2609)
- test-score stagnation [done] (`test-stagnation`, VAL-2609)
- compiler/linter stagnation [done] (same fail-fingerprint detector covers both)
- regression detection [done] (`evaluator-regression`, `edit-oscillation`)
- token/cost burn without progress [done] (`token-burn`, `cost-burn`)
- planning-without-execution detection [done] (`plan-without-exec`)
- file edit/revert oscillation [done] (`edit-oscillation`)
- semantic stagnation [later] (needs embeddings/LLM judge depth)
- configurable per-project detectors [done] (`ReflexDetectors`)
- detector confidence and evidence trails [done] (`Score` + `Evidence`)

Recovery actions:

- goal re-anchoring [done] (`recoveryPrompt`)
- replan [done] (family-conditioned via `PreferRecovery`)
- context compaction / failed-attempt summary [next] (replan prompt only)
- critic/debugger invocation [done] (preferred for repeated-error / regression)
- model switch [done] (native real escalate; sidecar ModelOverride inject)
- provider switch [done] (via model escalate fallback)
- agent switch [done] (v0.2 handoff)
- checkpoint rollback [done] (VAL-2608)
- alternate-approach fork [done] (VAL-2608, `ForkAttempt`)
- human escalation [done] (preferred for `discover:*`)
- intervention attempt limits / backoff [done] (VAL-2608, `LadderConfig`)

Shipped across VAL-2672 (family-conditioned selection), VAL-2609 (detectors), VAL-2608 (rollback/fork/limits + uplift benchmark). Remaining: semantic stagnation, full compaction summaries.

---

## v0.5 — Arbiter: adaptive routing [partial]

Routing inputs:

- task/repository classification [partial] (follow-up classifier only)
- language/framework detection [later] (no code)
- agent capabilities [done] (`agent.Capabilities`)
- provider/model capabilities [done] (static `provider.Capabilities`)
- context requirements [later] (no code)
- privacy constraints [partial] (`local_first` is the proxy; no constraint model)
- cost budgets [done] (`MaxCostPerTask`)
- latency targets [later] (no code)
- provider health [done] (`Discover` liveness)
- local resource availability [later] (no code)
- previous attempts [done] (`tried` set)
- historical success [later] (v0.11 work)

Routing features:

- deterministic policy rules [done] (VAL-2611)
- hard allow/deny constraints [done] (VAL-2611)
- local-first execution [done] (VAL-2611, enforced)
- fallback and escalation chains [done] (VAL-2611, `NextInChain`)
- planner / implementer / reviewer role assignment [later] (no code)
- model effort/reasoning controls [later] (no code)
- cost/token/latency accounting [done] (VAL-2611, `Ledger`)
- candidate scoring and explainable routing rationale [partial] (`Candidate.Score` is structural only; rationale narrated, emitted, and shown in TUI)
- provider health checks [done] (discovery probes)
- model catalog and capability discovery [partial] (per-provider `Models()`; no unified catalog)

**Exit criterion:** Temper can select among local and hosted model/agent combinations, explain the selection, and automatically escalate when the initial strategy stalls. [done] (VAL-2611)

---

## v0.6 — Extensibility, tools, skills, and policy [later]

Protocols and extension points (all [later]; `MCP`/`ACP` exist only as capability flags):

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
- plugin/hook API (note: orca agent hooks are external to Temper, not a Temper API)
- lifecycle hooks
- compaction hooks
- event subscribers
- extension discovery/catalog
- versioned extension manifests

Permissions and policy:

- allow / ask / deny rules [partial] (shell deny patterns; no ask flow)
- path-scoped file permissions [later]
- command-pattern shell permissions [done] (`Shell.Deny`)
- external-directory controls [later]
- network controls [later]
- provider/model allow/deny policy [done] (VAL-2611)
- per-agent permissions [partial] (routing allow/deny; no tool-scoped per-agent perms)
- per-tool permissions [later]
- explicit auto-approval mode [partial] (`ReflexAuto` covers recovery)
- secrets/environment filtering [later]
- secret redaction in events/logs [partial] (`temper config` display only)
- human approval boundaries [partial] (recovery approvals; no tool approvals)

**Exit criterion:** a third party can add an agent, provider, tool, skill, hook, or policy without modifying Temper core. [later]

---

## v0.7 — Remote and multi-machine execution [later]

Remote workspace support (all [later], no code):

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

Execution environments (all [later], no code):

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

- command/test logs [partial] (evaluator output stored as artifacts)
- screenshots [later]
- browser artifacts [later]
- videos where supported [later]
- structured completion evidence [later]

**Exit criterion:** a run can move between local and remote execution targets while preserving the same Temper task/event model. [later]

---

## v0.8 — Automations and external work sources [later]

Automations (all [later], no code):

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

Work-source integrations (all [later], no code; no `WorkSource` interface yet):

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

**Exit criterion:** Temper can accept a task from an external system, schedule or trigger execution, create an isolated workspace, run an agent, verify it, and publish the resulting change. [later]

---

## v0.9 — Operator control surface [partial]

The control surface is for **observing and steering the runtime**, not replacing a developer's editor.

CLI/TUI:

- project/task/session browser [partial] (session picker; no task browser)
- multiple conversations per task [partial] (sessions pane)
- agent/model/mode switchers [partial] (agent + model switching; no mode switcher)
- run timeline [partial] (event log pane)
- Reflex intervention display [done]
- Arbiter decision display [done] (VAL-2611 `routing` line)
- prompt queue [done] (follow-up input while running)
- command palette [partial] (prefix menu)
- prompt library [later]
- reusable task templates [later]
- notifications [later]
- resource monitor [partial] (tokens/budget footer)
- searchable logs [partial] (scroll; no search)

Code/workspace inspection:

- file tree [later]
- content search [later] (agent tool only, no TUI view)
- git status [later]
- diff viewer [later]
- commit list [later]
- PR/check summaries [partial] (CLI `pr status`; no TUI view)
- terminal sessions [later]
- persistent terminal scrollback [later]
- side-by-side/split views where useful [partial] (log/io/sessions panes)

Preview/testing (all [later], no code):

- dev-server detection
- local/remote port previews
- browser preview surface
- screenshot capture
- authenticated preview profiles
- UI annotation/target feedback as a later enhancement

Optional clients (all [later], no code):

- local web UI
- Tauri + Svelte desktop client
- editor extensions through protocol/API integration

**Exit criterion:** an operator can understand what every active agent is doing, inspect evidence, steer execution, and intervene without reading raw logs. [partial] (live-run observe + steer done; evidence inspection mostly CLI/raw)

---

## v0.10 — Evaluation, parallelism, and tournaments [later]

- parallel worktrees [later]
- parallel sub-tasks [later]
- workflow DAGs [later]
- planner / implementer / reviewer workflows [later]
- multiple candidate solutions [later]
- automated test/evaluator execution [done] (evaluator runs every step)
- LLM critic/judge as supplemental evidence [partial] (critic rung + SLM judge; not tournament-scoped)
- diff complexity scoring [later]
- candidate comparison [later]
- winning-solution selection [later]
- merge/rebase of parallel work [later]
- comparative agent/model analytics [later]
- benchmark packs [partial] (single reflex uplift benchmark)
- replay against historical tasks [later]

**Exit criterion:** Temper can execute multiple strategies for the same task and select a verified winner using explicit evaluation criteria. [later]

---

## v0.11 — Learn [later]

- policy version history [later]
- run/outcome dataset [partial] (`RunTelemetry` schema only, VAL-2649; no collection)
- strategy performance statistics [later]
- model/agent success priors [later]
- cost-normalized success metrics [later]
- local-vs-cloud escalation learning [later]
- Reflex threshold optimization [later]
- recovery-order optimization [later]
- workflow-combination scoring [later]
- offline historical replay [later]
- held-out benchmark evaluation [later]
- candidate policy generation [later]
- automatic recommendations [later]
- canary/opt-in policy rollout [later]
- rollback of policy versions [later]
- safe policy promotion [later]
- exploration budget / contextual-bandit experiments when data justifies them [later]

**Exit criterion:** historical data produces a policy change that improves held-out task performance without violating cost, privacy, or permission constraints. [later]

---

## v1.0 — Reliable agent control plane [later]

Stability and team-readiness:

- stable public plugin/adapter APIs [later]
- migration/versioning strategy [later]
- robust restart/crash recovery [partial] (`temper continue`; no crash recovery)
- distributed run locking [later]
- idempotent side effects [later]
- budgets and quotas [partial] (per-task cost cap only)
- audit trail [partial] (append-only event store; single-user)
- RBAC [later]
- shared organization policies [later]
- team agent/provider catalogs [later]
- shared skills/rules/templates [later]
- organization-wide routing policy [later]
- organization-level telemetry [later]
- cross-repository learning with privacy controls [later]
- hosted control-plane option [later]
- self-hosted server deployment [later]
- enterprise SSO/secrets integration [later]

## Cross-cutting requirements

These apply throughout the roadmap rather than belonging to one release:

- resumability over restart/reconnect [partial] (`continue`; no reconnect layer)
- clear capability discovery rather than hard-coded assumptions [partial] (some id-based locality remains)
- local-first support [done]
- observable state transitions [done] (transition events)
- event-backed auditability [done] (SQLite event store)
- safe cancellation and process cleanup [done]
- cross-platform path/process handling [partial] (Go builds; Unix-centric paths)
- configuration migration [later]
- resource accounting [done] (ledger + disk usage)
- explicit permissions [partial] (routing + shell deny; rest later)
- human override at every autonomous layer [done] (human reflex mode + approvals)
- no dependence on hidden model reasoning for progress detection [done] (heuristics-first judge)

## Product boundary

Temper should **integrate with** editors, terminals, browsers, issue trackers, forges, model providers and coding agents rather than attempting to replace all of them.

The differentiating center remains:

> **Observe execution, determine whether useful progress is happening, choose the best available strategy, recover when it is not, and improve those decisions from evidence over time.**
