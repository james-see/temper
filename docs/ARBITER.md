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
