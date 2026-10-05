package reflex

import "time"

// Ladder walks recovery actions with per-action and total attempt limits
// plus exponential backoff between interventions.
type Ladder struct {
	Steps    []string
	Attempts map[string]int
	MaxEach  int
	// Caps overrides MaxEach per action.
	Caps map[string]int
	// MaxTotal bounds interventions per run. Zero means unlimited.
	MaxTotal int
	// BackoffBase/BackoffMax space consecutive interventions: base,
	// 2*base, 4*base... capped at max. Zero base disables backoff.
	BackoffBase time.Duration
	BackoffMax  time.Duration
	total       int
	consec      int
	lastAt      time.Time
	now         func() time.Time
}

func NewLadder(actions []string) *Ladder {
	if len(actions) == 0 {
		actions = []string{"replan", "critic", "switch_model", "switch_agent", "human"}
	}
	return &Ladder{Steps: append([]string(nil), actions...), Attempts: map[string]int{}, MaxEach: 1}
}

// LadderConfig carries attempt-limit and backoff tuning.
type LadderConfig struct {
	MaxEach     int
	Caps        map[string]int
	MaxTotal    int
	BackoffBase time.Duration
	BackoffMax  time.Duration
}

// NewLadderWithConfig builds a ladder with explicit limits and backoff.
func NewLadderWithConfig(actions []string, c LadderConfig) *Ladder {
	l := NewLadder(actions)
	l.ApplyConfig(c)
	return l
}

// ApplyConfig replaces the ladder's limits and backoff. Zero values leave
// NewLadder defaults in place, except Caps which replaces the map.
func (l *Ladder) ApplyConfig(c LadderConfig) {
	if c.MaxEach > 0 {
		l.MaxEach = c.MaxEach
	}
	if c.Caps != nil {
		l.Caps = c.Caps
	}
	l.MaxTotal = c.MaxTotal
	l.BackoffBase = c.BackoffBase
	l.BackoffMax = c.BackoffMax
}

func (l *Ladder) cap(step string) int {
	if l.Caps != nil {
		if n, ok := l.Caps[step]; ok {
			return n
		}
	}
	return l.MaxEach
}

func (l *Ladder) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func (l *Ladder) stamp(step string) {
	l.Attempts[step]++
	l.total++
	l.consec++
	l.lastAt = l.clock()
}

func (l *Ladder) Next() (string, bool) {
	if l.MaxTotal > 0 && l.total >= l.MaxTotal {
		return "", false
	}
	for _, step := range l.Steps {
		if l.Attempts[step] < l.cap(step) {
			l.stamp(step)
			return step, true
		}
	}
	return "", false
}

// Cooldown reports how long until the next intervention is due. Zero means
// ready now. Progress resets the backoff via NoteProgress.
func (l *Ladder) Cooldown() time.Duration {
	if l.BackoffBase <= 0 || l.consec == 0 {
		return 0
	}
	wait := l.BackoffBase << (l.consec - 1)
	if l.BackoffMax > 0 && wait > l.BackoffMax {
		wait = l.BackoffMax
	}
	remain := wait - l.clock().Sub(l.lastAt)
	if remain < 0 {
		return 0
	}
	return remain
}

// NoteProgress resets consecutive-stall backoff after a progressing step.
func (l *Ladder) NoteProgress() {
	l.consec = 0
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
