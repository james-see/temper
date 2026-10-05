package arbiter

// RunTelemetry is the per-run record learned routing learns from. It is a
// schema only: nothing in this package writes it yet (see docs/ROUTING.md
// phase 1). Fields mirror the VAL-2649 telemetry list: who ran what, what
// it cost, how evaluation went, and whether the user accepted the result.
type RunTelemetry struct {
	// RunID is the temper run id.
	RunID string `json:"run_id"`
	// TaskType classifies the goal (bugfix, feature, refactor, research…).
	TaskType string `json:"task_type,omitempty"`
	// Agent, Provider, Model name the execution combination.
	Agent    string `json:"agent"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	// PromptTokens and CompletionTokens are worker totals for the run.
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	// LatencyMs is wall-clock run time in milliseconds.
	LatencyMs int64 `json:"latency_ms"`
	// CostUSD prices the run's tokens.
	CostUSD float64 `json:"cost_usd"`
	// TestsPassed and TestsFailed count evaluator outcomes.
	TestsPassed int `json:"tests_passed"`
	TestsFailed int `json:"tests_failed"`
	// ToolErrors carries fingerprinted tool-error ids and their counts.
	ToolErrors map[string]int `json:"tool_errors,omitempty"`
	// Completed reports whether the run finished successfully.
	Completed bool `json:"completed"`
	// Accepted records user acceptance when known (nil = unknown).
	Accepted *bool `json:"accepted,omitempty"`
}
