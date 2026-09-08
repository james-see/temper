# Event Model

Temper uses an append-only event stream as the source of truth for execution history.

## Why

Events enable:
- replay
- inspection
- auditability
- recovery analysis
- run comparison
- offline evaluation
- policy learning
- deterministic reconstruction of supervisor state

## Envelope

```go
type Event struct {
    ID        string
    RunID     string
    Sequence  uint64
    Type      string
    Timestamp time.Time
    Actor     string
    Data      json.RawMessage
}
```

## Initial taxonomy

### Run
- `run.created`
- `run.started`
- `run.cancelled`
- `run.completed`
- `run.failed`

### Task
- `task.classified`
- `task.acceptance_defined`

### Planning
- `plan.created`
- `plan.updated`

### Agent/model
- `agent.selected`
- `agent.started`
- `agent.stopped`
- `agent.switched`
- `model.called`
- `model.completed`

### Tools/workspace
- `tool.requested`
- `tool.completed`
- `file.modified`
- `workspace.diff`
- `checkpoint.created`
- `checkpoint.restored`

### Evaluation
- `test.executed`
- `compile.executed`
- `evaluation.completed`
- `progress.evaluated`

### Reflex/Arbiter
- `loop.detected`
- `stagnation.detected`
- `regression.detected`
- `strategy.changed`
- `routing.decided`
- `recovery.started`
- `recovery.completed`

## Replay

Replay reconstructs state from events and artifacts. External side effects are not automatically re-executed unless explicitly requested.

## Forking

A run can be forked from a checkpoint/event sequence into a new run. This is foundational for A/B strategy evaluation, rollback recovery, and agent tournaments.

## Ordering

Events must have a monotonically increasing sequence number scoped to the run. Wall-clock timestamps are useful metadata, but sequence order is authoritative.

## Artifacts

Large payloads such as diffs, logs, model transcripts and test output should be stored as content-addressed artifacts and referenced by events rather than duplicated into the event table.
