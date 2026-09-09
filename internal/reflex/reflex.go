package reflex

type ProgressState string

const (
	Progressing ProgressState = "progressing"
	Uncertain   ProgressState = "uncertain"
	Stalled     ProgressState = "stalled"
	Looping     ProgressState = "looping"
	Regressing  ProgressState = "regressing"
	Complete    ProgressState = "complete"
)

type Assessment struct {
	State   ProgressState `json:"state"`
	Score   float64       `json:"score"`
	Reasons []string      `json:"reasons,omitempty"`
	// Family is the normalized action family that triggered a loop detection
	// (e.g. "discover:linear" for an MCP tool-discovery loop). Empty when the
	// loop is not attributable to a single action family. Recovery prompts use
	// it to emit MCP-specific guidance instead of a generic replan nudge.
	Family string `json:"family,omitempty"`
}
