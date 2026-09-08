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
}
