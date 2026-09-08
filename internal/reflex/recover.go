package reflex

type Ladder struct {
	Steps    []string
	Attempts map[string]int
	MaxEach  int
}

func NewLadder(actions []string) *Ladder {
	if len(actions) == 0 {
		actions = []string{"replan", "critic", "switch_model", "human"}
	}
	filtered := make([]string, 0, len(actions))
	for _, a := range actions {
		if a == "switch_agent" {
			continue
		}
		filtered = append(filtered, a)
	}
	return &Ladder{Steps: filtered, Attempts: map[string]int{}, MaxEach: 1}
}

func (l *Ladder) Next() (string, bool) {
	for _, step := range l.Steps {
		if l.Attempts[step] < l.MaxEach {
			l.Attempts[step]++
			return step, true
		}
	}
	return "", false
}

func (l *Ladder) Current() string {
	var last string
	for _, step := range l.Steps {
		if l.Attempts[step] > 0 {
			last = step
		}
	}
	return last
}
