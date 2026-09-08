package reflex

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

type Signals struct {
	Actions      []string
	Errors       []string
	Tokens       int
	TokenBurn    int
	EvalPassed   *bool
	PrevEvalPass *bool
	Meaningful   bool
	StagnationN  int
}

type Engine struct {
	ActionThresh int
	ErrorThresh  int
	TokenBurn    int
	Stagnation   int
	Regression   bool
	actions      []string
	errors       []string
}

func NewEngine(actionRep, errorThresh, tokenBurn, stagnation int, regression bool) *Engine {
	if actionRep <= 0 {
		actionRep = 2
	}
	if errorThresh <= 0 {
		errorThresh = 3
	}
	if tokenBurn <= 0 {
		tokenBurn = 20000
	}
	if stagnation <= 0 {
		stagnation = 6
	}
	return &Engine{
		ActionThresh: actionRep,
		ErrorThresh:  errorThresh,
		TokenBurn:    tokenBurn,
		Stagnation:   stagnation,
		Regression:   regression,
	}
}

func (e *Engine) ObserveAction(norm string) {
	if norm != "" {
		e.actions = append(e.actions, norm)
	}
}

func (e *Engine) ObserveError(fp string) {
	if fp != "" {
		e.errors = append(e.errors, fp)
	}
}

func (e *Engine) Assess(sig Signals) Assessment {
	actions := append(append([]string{}, e.actions...), sig.Actions...)
	errors := append(append([]string{}, e.errors...), sig.Errors...)

	if n, ok := tailRepeat(actions); ok && n >= e.ActionThresh {
		return Assessment{State: Looping, Score: 0.1, Reasons: []string{"repeated-action"}}
	}
	if n, ok := tailRepeat(errors); ok && n >= e.ErrorThresh {
		return Assessment{State: Looping, Score: 0.1, Reasons: []string{"repeated-error"}}
	}
	if e.Regression && sig.PrevEvalPass != nil && sig.EvalPassed != nil && *sig.PrevEvalPass && !*sig.EvalPassed {
		return Assessment{State: Regressing, Score: 0.15, Reasons: []string{"evaluator-regression"}}
	}
	tokens := sig.Tokens
	if tokens == 0 {
		tokens = sig.TokenBurn
	}
	if tokens >= e.TokenBurn && !sig.Meaningful {
		return Assessment{State: Stalled, Score: 0.2, Reasons: []string{"token-burn"}}
	}
	if sig.StagnationN >= e.Stagnation && !sig.Meaningful {
		return Assessment{State: Stalled, Score: 0.25, Reasons: []string{"stagnation"}}
	}
	if sig.EvalPassed != nil && *sig.EvalPassed {
		return Assessment{State: Complete, Score: 1, Reasons: []string{"evaluator-passed"}}
	}
	if sig.Meaningful {
		return Assessment{State: Progressing, Score: 0.7, Reasons: []string{"workspace-delta"}}
	}
	if len(actions) < 2 {
		return Assessment{State: Progressing, Score: 0.5, Reasons: []string{"starting"}}
	}
	return Assessment{State: Uncertain, Score: 0.4, Reasons: []string{"semantic-stagnation"}}
}

func NormalizeAction(name, args string) string {
	return name + ":" + compact(args)
}

func FingerprintError(s string) string {
	s = compact(s)
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func compact(s string) string {
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		space = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

func tailRepeat(items []string) (int, bool) {
	if len(items) == 0 {
		return 0, false
	}
	last := items[len(items)-1]
	n := 0
	for i := len(items) - 1; i >= 0; i-- {
		if items[i] != last {
			break
		}
		n++
	}
	return n, n > 1
}
