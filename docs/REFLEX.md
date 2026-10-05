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
No meaningful git-tree or evaluator delta after N costly actions. Wired: identical repo-state hash across `repo_stagnation.actions` assessments (default 4) reports `repo-stagnation`.

### Regression
A previously achieved evaluator state gets worse. Wired: evaluator pass→fail flips (`evaluator-regression`) and A-B-A file content reverts (`edit-oscillation`, on by default).

### Token burn
Spend/tokens exceed a configured ratio to progress. Wired: completion-token burn (`token_burn.threshold`, default 20000) and, when `cost_burn.usd` is set, cumulative spend.

### Test stagnation
The same failing evaluator output repeats `test_stagnation.threshold` times (default 3) without changing.

### Planning loop
Plans or strategy summaries recur while no external action happens. Wired: `plan_without_exec.actions` (default 3) consecutive think-only assessments with substantial thinking report `plan-without-exec`; rumination keeps longer thinking.

Every firing detector attaches an evidence trail (`Assessment.Evidence`: detector + detail) alongside its confidence score.

### Semantic stagnation
Different surface actions produce essentially the same state. Initially use state fingerprints; later optional model-assisted classification.

Do **not** fire this on the first handful of steps, or when the agent is using exploratory tools (`ls`, `pwd`, `read`, `search`, `git log/status/diff`). Those are progress unless the exact same call repeats. `Uncertain` is not failure: continue, or accept a final model answer when no evaluator is configured. Only `Stalled` / `Looping` / `Regressing` start the recovery ladder.

### Token burn
Count **completion** tokens without progress, not cumulative prompt/prefill. Local models (Ollama/MLX) re-send the transcript each turn; summing prompt tokens looks like a 40k runaway when it is just prefill. A generate still in flight, or a step with `out 0`, is not a stall.

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

Default order:

1. Re-anchor on unresolved goal
2. Replan
3. Invoke critic/debugger
4. Switch model (native: real provider/model change; sidecars with `ModelOverride`: record + inject)
5. Switch agent
6. Roll back to the last progressing checkpoint (`rollback`)
7. Fork: preserve the attempt on a branch, reset, try a new approach (`fork`)
8. Human escalation

Limits: per-action caps (`max_attempts`, default 1), a run total (`recovery_max_attempts`, default 10), and optional exponential backoff (`recovery_backoff_seconds`, default off) with `recovery.deferred` events while cooling down. Mode `off` keeps detection but never intervenes. Rollback/fork need run checkpoints, so they are native-only today.

Temper also **conditions the next rung on the detector family/reason** (`PreferRecovery` / `Ladder.NextFor`):

| Signal | Prefer first |
|---|---|
| `discover:*` MCP tool-discovery loop | `human` (auth), then critic |
| `rumination` | `switch_model` |
| `repeated-error` | `critic` |
| `repeated-action` / `repeated-cycle` | `replan` |
| `token-burn` | `switch_model` |
| `stagnation` | `replan` |
| regression | `critic` |

Exhausted preferred rungs fall through to the remaining ladder steps. Every intervention emits an event and records why it happened.

## Critical invariant

Recovery itself must not become a loop. Reflex needs attempt counters, backoff, budget limits, maximum escalation policies, and a human boundary.
