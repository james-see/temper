# Arbiter

Arbiter is Temper's routing and policy engine.

## Responsibilities

Arbiter selects:
- agent
- provider
- model
- role/strategy
- fallback
- critic
- reviewer
- escalation path

## Inputs

```text
Task features
Repository features
Agent capabilities
Model capabilities
User policy
Budget
Privacy constraints
Provider health
Historical performance
Current run state
Previous failed attempts
```

## Initial implementation

Start deterministic and explainable.

A routing decision should produce:
1. candidate set
2. exclusions
3. weighted scores
4. selected candidate
5. machine-readable rationale

Example:

```yaml
arbiter:
  policies:
    - match:
        complexity: low
      prefer:
        agent: opencode
        provider: local-mlx

    - match:
        language: go
        task: bugfix
      prefer:
        agent: codex
```

## Routing objective

```text
utility =
  P(success) * quality
  - λ_cost * expected_cost
  - λ_latency * expected_latency
  - λ_risk * policy_risk
```

Users should be able to change those weights or define hard constraints.

## Later optimization

Do not jump directly to opaque reinforcement learning. Start with:
- success-rate estimates
- Bayesian/weighted priors
- cost-normalized scores
- exploration budget
- contextual bandit experiments when enough data exists

Historical outcomes can update priors for combinations such as:

```text
(task class, language, agent, provider, model, strategy)
```

## Explainability

Every routing decision should answer:

```text
Why this agent?
Why this model/provider?
Which candidates were rejected?
Which policy matched?
What would trigger escalation?
```

That rationale should be emitted as a `routing.decided` event and remain available during replay.

## Deterministic policy + escalation (implemented, VAL-2611)

Routing precedence, highest first:

1. Hard policy deny (`deny_providers`, `deny_agents`)
2. Hard policy allow (`allow_providers`, `allow_agents`; empty means all)
3. Explicit CLI/agent selections
4. Local-first (`temper.preference.local_first` picks a usable local
   provider over a non-local default)
5. Default discovery order

Deny wins over allow, and both win over explicit selections. Every override
is narrated in the decision's `reasons`, which are emitted on the
`routing.decided` event and shown as the `routing` line in the TUI status
overlay. Policy `match` is reserved for future conditional policies;
allow/deny apply globally.

When Reflex reports stall, the `switch_model` recovery rung escalates down
`arbiter.escalation` (then default discovery order), skipping tried,
unusable, and policy-blocked providers. Each skip and the final pick carry
reasons, so escalation reads as a chain: `chain X selected (position n/m)`.

Cost/latency accounting (`internal/arbiter/ledger.go`) accumulates
per-provider calls, tokens, errors, and latency over a run and rides on
`routing.decided` payloads as `accounting`. Token cost uses the coarse
`CostPerTokenUSD` heuristic until per-model rates land.
