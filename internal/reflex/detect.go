package reflex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode"
)

const earlyGrace = 4

type Signals struct {
	Actions          []string
	Errors           []string
	Tokens           int
	TokenBurn        int
	PromptTokens     int
	CompletionTokens int
	EvalPassed       *bool
	PrevEvalPass     *bool
	Meaningful       bool
	Exploratory      bool
	StagnationN      int
	Step             int
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
	exploratory := sig.Exploratory || anyExploratory(sig.Actions)
	early := (sig.Step > 0 && sig.Step <= earlyGrace) || (sig.Step == 0 && len(actions) < earlyGrace)
	distinct := distinctCount(actions)

	if n, ok := tailRepeat(actions); ok && n >= e.ActionThresh {
		return Assessment{State: Looping, Score: 0.1, Reasons: []string{"repeated-action"}}
	}
	if n, ok := tailRepeat(errors); ok && n >= e.ErrorThresh {
		return Assessment{State: Looping, Score: 0.1, Reasons: []string{"repeated-error"}}
	}
	if e.Regression && sig.PrevEvalPass != nil && sig.EvalPassed != nil && *sig.PrevEvalPass && !*sig.EvalPassed {
		return Assessment{State: Regressing, Score: 0.15, Reasons: []string{"evaluator-regression"}}
	}
	if sig.EvalPassed != nil && *sig.EvalPassed {
		return Assessment{State: Complete, Score: 1, Reasons: []string{"evaluator-passed"}}
	}
	if sig.Meaningful {
		return Assessment{State: Progressing, Score: 0.7, Reasons: []string{"workspace-delta"}}
	}
	if exploratory {
		return Assessment{State: Progressing, Score: 0.65, Reasons: []string{"exploring"}}
	}
	if early || (distinct < earlyGrace && sig.Step <= earlyGrace) {
		return Assessment{State: Progressing, Score: 0.55, Reasons: []string{"early-observation"}}
	}
	if len(actions) < 2 {
		return Assessment{State: Progressing, Score: 0.5, Reasons: []string{"starting"}}
	}
	// Prefill (prompt tokens) is not progress failure. Only completion-token
	// burn after the grace window, with no workspace or explore evidence.
	if sig.CompletionTokens >= e.TokenBurn && sig.CompletionTokens > 0 {
		return Assessment{State: Stalled, Score: 0.2, Reasons: []string{"token-burn"}}
	}
	if sig.StagnationN >= e.Stagnation {
		return Assessment{State: Stalled, Score: 0.25, Reasons: []string{"stagnation"}}
	}
	return Assessment{State: Uncertain, Score: 0.4, Reasons: []string{"semantic-stagnation"}}
}

func IsExploratory(name, args string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "search":
		return true
	case "git":
		return gitExplore(args)
	case "shell":
		return shellExplore(args)
	default:
		return false
	}
}

func anyExploratory(actions []string) bool {
	for _, a := range actions {
		name, rest, _ := strings.Cut(a, ":")
		if IsExploratory(name, rest) {
			return true
		}
	}
	return false
}

func gitExplore(args string) bool {
	var in struct {
		Args    []string `json:"args"`
		Command string   `json:"command"`
	}
	_ = json.Unmarshal([]byte(args), &in)
	cmd := ""
	if len(in.Args) > 0 {
		cmd = in.Args[0]
	} else {
		cmd = firstWord(in.Command)
	}
	switch strings.ToLower(cmd) {
	case "log", "status", "diff", "show", "rev-parse":
		return true
	}
	return false
}

func shellExplore(args string) bool {
	var in struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal([]byte(args), &in)
	cmd := strings.TrimSpace(in.Command)
	if i := strings.IndexAny(cmd, "|&;"); i >= 0 {
		cmd = strings.TrimSpace(cmd[:i])
	}
	word := firstWord(cmd)
	switch word {
	case "ls", "pwd", "cat", "head", "tail", "file", "wc", "find", "rg", "grep", "tree", "which", "stat", "realpath", "dirname", "basename":
		return true
	case "git":
		sub := firstWord(strings.TrimSpace(strings.TrimPrefix(cmd, "git")))
		switch sub {
		case "log", "status", "diff", "show", "rev-parse":
			return true
		}
	}
	return false
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return strings.ToLower(s[:i])
	}
	return strings.ToLower(s)
}

func distinctCount(items []string) int {
	seen := map[string]struct{}{}
	for _, i := range items {
		if i != "" {
			seen[i] = struct{}{}
		}
	}
	return len(seen)
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
