# Model/Provider Abstraction and Self-Optimizing Routing

Design exploration for VAL-2649. As-is references are file:line against the
post-VAL-2611 tree. This ticket is design-only: the only new code is the
telemetry schema (`internal/arbiter/telemetry.go`), which is types without
wiring. Learned routing is explicitly not implemented here.

## 1. As-is inventory

**Provider interface** (`internal/provider/provider.go:74-79`): `Provider`
is already protocol-agnostic — `ID`, `Models`, `Capabilities`, and a
streaming `Generate` over OpenAI-shaped `Message`/`ToolCall`/`Usage` types.
`Capabilities` (`provider.go:16-28`) already carries context window, tool
use, vision, reasoning, streaming, and locality.

**Registry** (`internal/provider/registry.go:14`, `discover.go:412`):
`type` selects the protocol driver. `openai` takes any base URL, so
OpenAI-compatible endpoints need no bespoke adapter: oMLX reuses the OpenAI
client outright (`openai.go:27-35`), and OpenRouter is a URL default
(`registry.go:16-21`).

**Discovery and order** (`discover.go:98`, `discover.go:70`): `Discover`
probes each configured id and marks it usable; `PreferredOrder` ranks ids
(default provider, then locals under `local_first`, then cloud, then the
rest). Locality today is id-based (`ollama`, `local-mlx`).

**Arbiter** (`internal/arbiter/`): `SelectWithStatus` (`select.go:17`)
routes with precedence deny > allow > explicit > local-first > discovery
order; `NextInChain` (`chain.go:37`) walks `arbiter.escalation` for
stall-triggered fallback; `Ledger` (`ledger.go:40`) accumulates
per-provider calls/tokens/errors/latency. Every override is narrated in
`Decision.Reasons`, emitted on `routing.decided` and shown in the TUI.

**Agent execution** (`internal/agent/agent.go:34-51`): `Agent`
(`Start`/`Resume`/`Interrupt`/`Events` + `Capabilities`) with `Sidecar`
(`Poll`/`Inject`/`SessionID`/`LaunchLine`) on top. Seven harnesses implement
it (Hermes, Cursor, OpenCode, Muse, Goose, Claude Code, Codex) plus a
generic `exec` adapter, so harnesses are already interchangeable backends.
`ModelOverride` advertises sidecars that accept routed models.

**Config** (`internal/config/config.go`): `Provider` (`:39`) carries
`type/url/cloud_url/key`; `Preference` (`:34`) carries `local_first` and
`default_provider`; `Policy` (`:80`) carries hard allow/deny;
`Arbiter.Escalation` (`:77`) carries the fallback chain.

## 2. Proposed provider/protocol/model interfaces

Keep `provider.Provider` as the single protocol interface; it already
abstracts what routing needs. The work is in configuration, not Go types:

1. **Protocol stays config-selected.** `type: openai` + `url` is the generic
   OpenAI-compatible driver (Z.AI, oMLX, OpenRouter, any `/chat/completions`
   endpoint). No new driver per vendor. Anthropic- and Gemini-shaped
   endpoints keep their drivers until a second vendor needs them.
2. **Per-id default model.** `DefaultModel` (`discover.go:369`) switches on
   *type*, so a Z.AI entry (`type: openai`) wrongly defaults to
   `gpt-4.1-mini`. Proposed: `default_model` on `config.Provider`, checked
   before the type switch. Until then, Z.AI runs require an explicit model.
3. **Generic OpenAI-compatible discovery.** `Discover` hand-probes known
   ids; a new provider id is usable for execution but invisible to
   discovery-driven selection. Proposed: for `type: openai` entries with a
   key, probe `GET {url}/models` and mark usable on 200. That makes new
   providers config-only end to end.
4. **Declared locality and cost.** Replace id-based locality with `local:
   true` on `config.Provider`, and add optional `cost_per_mtok: {input,
   output}`. Routing then orders by (policy, locality, cost) from data
   instead of hardcoded ids, and the ledger prices runs from the same
   table, retiring the `CostPerTokenUSD` heuristic.
5. **Capability metadata.** Merge live `/models` output with configured
   per-model capabilities (reasoning, vision, tools, context window,
   latency class) so the arbiter can match task needs (e.g. vision) to
   models instead of ids.

## 3. Proposed agent execution interface

No new interface: `Agent` + `Sidecar` already separate harness mechanics
from routing. Two standardizations complete the picture:

1. **Model as a routed dimension for sidecars.** `TaskRequest.Model` plus
   the `ModelOverride` capability is the seam; harnesses that accept it
   (today: native + overrides) take part in `switch_model` escalation,
   others stay fixed-model. Document per-adapter support in
   `docs/INTEGRATIONS.md` rather than changing the interface.
2. **Capability-driven harness choice.** `agent.Capabilities` (streaming,
   tools, MCP/ACP, resume, subagents, worktrees) is what a future
   task-class policy matches on; the `match` field reserved on
   `config.Policy` is where those rules will live.

## 4. Routing and fallback strategy

Current deterministic strategy (shipped, VAL-2611):

- Initial: policy deny > policy allow > explicit selection > local-first >
  discovery order, fully narrated.
- Stall: `switch_model` rung escalates down `arbiter.escalation`, skipping
  tried/unusable/policy-blocked providers.
- Accounting: ledger per provider, surfaced on `routing.decided`.

Initial multi-backend strategy on top of that (config, no code):

1. Put the zero-cost tier first: `glm-4.7-flash` ($0 in/out, §6) ahead of
   paid cloud for routine tasks; keep local Ollama first when
   `local_first` is set for privacy.
2. Escalate paid tiers by cost: Flash → Air/mini → flagship within a
   vendor, then across vendors per `escalation`.
3. Reserve capability matching (vision → `glm-4.6v-flash`, reasoning-heavy
   → flagship) for the capability-metadata proposal in §2.5.

`examples/routing.yaml` encodes this for all five backends.

## 5. Telemetry schema

`internal/arbiter/telemetry.go` defines `RunTelemetry`: run id, task type,
agent/provider/model, prompt/completion tokens, latency, cost, evaluator
pass/fail counts, tool-error fingerprints, and user accept/reject. It is
JSON-tagged for store/event persistence but nothing writes it yet — the
collection wiring is the first implementation milestone below, not this
ticket.

## 6. Learned-routing recommendation

Evolve in gated phases; each phase must keep every deterministic guarantee
(policy deny always wins, local-first preserved, cost caps enforced):

0. **Deterministic (now).** Rules + chains + narrated reasons. This is the
   fallback every later phase degrades to.
1. **Log telemetry.** Append `RunTelemetry` per run to the event store;
   derive task type from the existing goal classifier. No behavior change.
2. **Offline priors.** Nightly or on-demand: success rate and cost per
   (task type, agent, provider, model). Surface as `temper inspect`
   output before any automatic use.
3. **Assisted selection.** When priors for a task type clear a confidence
   bar, suggest (not enforce) the best combination in the TUI with its
   evidence; log acceptance as the `Accepted` signal.
4. **Contextual bandit.** Epsilon-greedy over eligible combinations with an
   exploration budget (cap: extra cost vs deterministic pick), constrained
   to policy-allowed providers. Record every exploration for replay.
5. **Full self-improvement only with eval gates.** Promote a learned policy
   to default only when it beats deterministic routing on a held-out task
   set on success rate at equal-or-lower cost.

Do not skip to 4/5: without phases 1–3 there is no ground truth, and the
current per-run volume cannot support online learning claims.

## 7. Z.AI / GLM evaluation

Researched 2026-10-04 against docs.z.ai (primary) plus third-party setup
reports (secondary, noted where used).

**API shape (primary).** Fully OpenAI-compatible: base URL
`https://api.z.ai/api/paas/v4/`, `ZAI_API_KEY` env convention,
`/chat/completions` with streaming and function calling
([OpenAI guide](https://docs.z.ai/guides/develop/openai/python)). Temper
needs no adapter: `type: openai`, that URL, and a key reuse
`NewOpenAI` verbatim. Verified code path, not yet called live.

**Free tier (primary).** The [pricing table](https://docs.z.ai/guides/overview/pricing)
lists `glm-4.7-flash`, `glm-4.5-flash` (text) and `glm-4.6v-flash` (vision)
as Free on input, cached input, and output — a genuine zero-cost cloud
execution tier. Paid flagships for scale-up: `glm-4.7` ($0.60/$2.20 per
1M) and `glm-5.3`/`glm-5.2` ($1.40/$4.40). The ticket's GLM-4.7-Flash pick
stands; note GLM-5.x now leads the lineup.

**Cursor pattern (primary).** The [Cursor guide](https://docs.z.ai/devpack/tool/cursor)
is exactly Temper's openai-with-URL shape: OpenAI protocol + key + base-URL
override (`https://api.z.ai/api/coding/paas/v4` for Coding Plan
subscriptions) + model id. (Cursor quirk, not reusable: model names must be
uppercase there.) Nothing to build; it validates the config-only approach.

**OpenCode pattern (primary).** The [OpenCode guide](https://docs.z.ai/devpack/tool/opencode)
shows `opencode auth login` with native `Z.AI` / `Z.AI Coding Plan`
options — precedent for giving Z.AI a first-class named entry (as
`examples/routing.yaml` does) rather than burying it as a URL override.

**Reuse notes for Temper.**

- Reuse as-is: OpenAI-compatible chat/completions + tools + streaming
  (Temper's `OpenAI` client), key-in-env convention (`ZAI_API_KEY` →
  propose a `providers.zai.key` env mapping mirroring `OPENAI_API_KEY`).
- Coding Plan users point the same entry at `/api/coding/paas/v4`.
- Anthropic-compatible endpoint (`https://api.z.ai/api/anthropic`) is
  reported by third parties only — unverified against primary docs; do
  not depend on it until confirmed.
- Rate/concurrency limits for free models are console-only (secondary
  reports); treat the free tier as best-effort with paid escalation ready.

**Verdict.** Z.AI is the recommended zero-cost cloud tier: config-only
integration, free Flash models with tool calling, documented Cursor and
OpenCode patterns that mirror Temper's existing seams. Remaining gaps are
the §2 proposals (`default_model`, generic discovery, `ZAI_API_KEY`
mapping), each small and independent.
