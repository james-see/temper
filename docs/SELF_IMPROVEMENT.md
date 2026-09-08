# Self-Improvement

Temper's self-improvement model is **measured orchestration improvement**, not uncontrolled self-modifying code.

## Data collected per run

- task/repository classification
- selected agent/provider/model
- policy and prompt versions
- tool sequence
- Reflex detections/interventions
- evaluator trajectory
- tokens/cost/latency
- final outcome
- human overrides
- final diff/acceptance result

## Improvement loop

```text
production runs / benchmarks
          ↓
structured outcome dataset
          ↓
candidate policy changes
          ↓
historical replay + benchmark evaluation
          ↓
does candidate outperform baseline?
          ↓ yes
canary / opt-in
          ↓
promotion
```

## Initial learning targets

1. Agent selection
2. Model selection
3. Local-vs-cloud escalation threshold
4. Reflex detector thresholds
5. Recovery ordering
6. Planner/executor/reviewer combinations
7. Context/tool strategy

## Guardrails

A learned policy must never silently override hard constraints:
- max budget
- local-only/privacy rules
- provider deny lists
- forbidden tools
- network policy
- human-approval boundaries

## Evaluation metrics

Primary:
- verified task success rate

Secondary:
- cost per successful task
- latency per successful task
- interventions per task
- regressions introduced
- human-escalation rate
- diff size/complexity where meaningful

## `temper learn`

Long-term CLI concept:

```text
$ temper learn

Analyzed 417 runs

Go bugfixes:
  Codex success: 87%, $0.42 avg
  Claude success: 91%, $0.71 avg
  Local success: 64%, $0.03 avg

Recovery:
  model switch after 3 repeated errors: +27% success
  model switch after 5 repeated errors: +4% success

Suggested policy updates:
  - route low-complexity Go fixes local-first
  - escalate after 3 repeated identical errors
  - prefer Codex when local attempt stalls
```

Recommendations should be explainable and reviewable before activation.
