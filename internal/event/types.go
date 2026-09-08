package event

const (
	RunCreated    = "run.created"
	RunStarted    = "run.started"
	RunCancelled  = "run.cancelled"
	RunCompleted  = "run.completed"
	RunFailed     = "run.failed"

	TaskClassified         = "task.classified"
	TaskAcceptanceDefined  = "task.acceptance_defined"

	PlanCreated = "plan.created"
	PlanUpdated = "plan.updated"

	AgentSelected = "agent.selected"
	AgentStarted  = "agent.started"
	AgentStopped  = "agent.stopped"
	AgentSwitched = "agent.switched"
	ModelCalled   = "model.called"
	ModelCompleted = "model.completed"

	ToolRequested   = "tool.requested"
	ToolCompleted   = "tool.completed"
	FileModified    = "file.modified"
	WorkspaceDiff   = "workspace.diff"
	CheckpointCreated  = "checkpoint.created"
	CheckpointRestored = "checkpoint.restored"

	TestExecuted         = "test.executed"
	CompileExecuted      = "compile.executed"
	EvaluationCompleted  = "evaluation.completed"
	ProgressEvaluated    = "progress.evaluated"

	LoopDetected       = "loop.detected"
	StagnationDetected = "stagnation.detected"
	RegressionDetected = "regression.detected"
	StrategyChanged    = "strategy.changed"
	RoutingDecided     = "routing.decided"
	RecoveryStarted    = "recovery.started"
	RecoveryCompleted  = "recovery.completed"
)
