package arbiter

type Candidate struct {
	Agent    string  `json:"agent"`
	Provider string  `json:"provider,omitempty"`
	Model    string  `json:"model,omitempty"`
	Score    float64 `json:"score"`
}

type Decision struct {
	Selected   Candidate   `json:"selected"`
	Considered []Candidate `json:"considered,omitempty"`
	Reasons    []string    `json:"reasons,omitempty"`
}
