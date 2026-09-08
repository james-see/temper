# Reflex

Reflex is Temper's progress, loop-detection and recovery subsystem.

## Key idea

Temper does not inspect hidden chain of thought. It judges **observable execution state**.

## Progress signals

Positive:
- failing test count decreases
- compiler errors decrease
- acceptance criteria become satisfied
- meaningful repository diff appears
- new useful evidence is discovered
- evaluator score improves

Negative:
- same error fingerprint recurs
- same files are edited/reverted repeatedly
- same tool sequence repeats
- tests oscillate
- repository state barely changes
- token/cost burn rises without measurable progress
- planning messages repeat without execution
- previously passing tests fail

## State model

```go
type ProgressState string

const (
    Progressing ProgressState = "progressing"
    Uncertain   ProgressState = "uncertain"
    Stalled     ProgressState = "stalled"
    Looping     ProgressState = "looping"
    Regressing  ProgressState = "regressing"
    Complete    ProgressState = "complete"
)
```

## Detectors

### Exact action loop
Same normalized action repeated N times.

### Cyclic action loop
A sequence such as `edit → test → revert` repeats.

### Error loop
Same normalized compiler/test/runtime error recurs despite attempted fixes.

### Repository stagnation
No meaningful git-tree or evaluator delta after N costly actions.

### Regression
A previously achieved evaluator state gets worse.

### Token burn
Spend/tokens exceed a configured ratio to progress.

### Planning loop
Plans or strategy summaries recur while no external action happens.

### Semantic stagnation
Different surface actions produce essentially the same state. Initially use state fingerprints; later optional model-assisted classification.

## Progress score

```text
progress =
  w_tests * Δtest_score
+ w_compile * Δcompile_score
+ w_acceptance * Δacceptance
+ w_repo * meaningful_diff
+ w_evidence * new_information
- w_repeat * repeated_actions
- w_error * repeated_errors
- w_regress * regressions
- w_cost * normalized_cost_without_progress
```

The score should be pluggable per task/repository rather than pretending one universal metric is perfect.

## Recovery ladder

1. Re-anchor on unresolved goal
2. Replan
3. Invoke critic/debugger
4. Switch model
5. Switch agent
6. Roll back and fork alternate approach
7. Human escalation

Every intervention emits an event and records why it happened.

## Critical invariant

Recovery itself must not become a loop. Reflex needs attempt counters, backoff, budget limits, maximum escalation policies, and a human boundary.
