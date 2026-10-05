package reflex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

const (
	earlyGrace     = 4
	ThinkUncertain = 2500
	ThinkStalled   = 6000
)

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
	ThinkChars       int
	ThinkOnly        bool
	ThinkText        string
	// RepoHash fingerprints the workspace status/diff; empty means unknown.
	RepoHash string
	// EvalFingerprint fingerprints the latest evaluator output; empty when
	// no evaluator ran this step.
	EvalFingerprint string
	// CostUSD is the cumulative session spend; zero disables cost detection
	// unless a cost threshold is configured and exceeded.
	CostUSD float64
	// Writes carries content hashes of files written this step.
	Writes []FileWrite
}

// FileWrite identifies one file write by content hash.
type FileWrite struct {
	Path string
	Hash string
}

type Engine struct {
	ActionThresh int
	ErrorThresh  int
	TokenBurn    int
	Stagnation   int
	Regression   bool
	// RepoStagnation fires after this many assessments with an identical
	// non-empty repo hash. Zero disables.
	RepoStagnation int
	// TestStagnation fires after this many consecutive failures sharing one
	// evaluator fingerprint. Zero disables.
	TestStagnation int
	// CostBurnUSD fires once cumulative spend reaches it. Zero disables.
	CostBurnUSD float64
	// PlanWithoutExec fires after this many consecutive think-only
	// assessments with substantial thinking. Zero disables.
	PlanWithoutExec int
	// EditOscillation enables A-B-A revert detection across file writes.
	EditOscillation bool
	actions         []string
	errors          []string
	thinks          []string
	repoHash        string
	repoSame        int
	evalFP          string
	evalFailRun     int
	thinkOnlyRun    int
	writeHist       map[string][]string
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
		ActionThresh:    actionRep,
		ErrorThresh:     errorThresh,
		TokenBurn:       tokenBurn,
		Stagnation:      stagnation,
		Regression:      regression,
		RepoStagnation:  4,
		TestStagnation:  3,
		PlanWithoutExec: 3,
		EditOscillation: true,
	}
}

// DetectorTuning overrides the newer detector thresholds after NewEngine.
// It mirrors config.ReflexDetectors without importing the config package.
type DetectorTuning struct {
	RepoStagnation  int
	TestStagnation  int
	CostBurnUSD     float64
	PlanWithoutExec int
	EditOscillation bool
}

// ApplyTuning replaces every newer-detector threshold. Callers pass fully
// defaulted values; NewEngine's defaults cover direct construction.
func (e *Engine) ApplyTuning(t DetectorTuning) {
	e.RepoStagnation = t.RepoStagnation
	e.TestStagnation = t.TestStagnation
	e.CostBurnUSD = t.CostBurnUSD
	e.PlanWithoutExec = t.PlanWithoutExec
	e.EditOscillation = t.EditOscillation
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

func (e *Engine) Reset() {
	e.actions = nil
	e.errors = nil
	e.thinks = nil
	e.repoHash = ""
	e.repoSame = 0
	e.evalFP = ""
	e.evalFailRun = 0
	e.thinkOnlyRun = 0
	e.writeHist = nil
}

// observeRepo counts consecutive assessments sharing one repo hash. Empty
// hashes carry no information and reset the run.
func (e *Engine) observeRepo(hash string) {
	if hash == "" {
		e.repoHash = ""
		e.repoSame = 0
		return
	}
	if hash == e.repoHash {
		e.repoSame++
		return
	}
	e.repoHash = hash
	e.repoSame = 1
}

// observeEval counts consecutive failures sharing one evaluator fingerprint.
// A pass, or a step with no evaluation, resets the run.
func (e *Engine) observeEval(passed *bool, fp string) {
	if passed == nil || *passed || fp == "" {
		e.evalFailRun = 0
		if passed != nil && *passed {
			e.evalFP = ""
		}
		return
	}
	if fp == e.evalFP {
		e.evalFailRun++
		return
	}
	e.evalFP = fp
	e.evalFailRun = 1
}

// observeWrites records content hashes per path and reports a path whose
// last three writes form an A-B-A revert, or "" when none did.
func (e *Engine) observeWrites(writes []FileWrite) string {
	osc := ""
	for _, w := range writes {
		if w.Path == "" || w.Hash == "" {
			continue
		}
		if e.writeHist == nil {
			e.writeHist = map[string][]string{}
		}
		h := append(e.writeHist[w.Path], w.Hash)
		if len(h) > 3 {
			h = h[len(h)-3:]
		}
		e.writeHist[w.Path] = h
		if len(h) == 3 && h[0] == h[2] && h[0] != h[1] {
			osc = w.Path
		}
	}
	return osc
}

func ev(detector, format string, args ...any) []Evidence {
	return []Evidence{{Detector: detector, Detail: fmt.Sprintf(format, args...)}}
}

func thinkAssessment(chars int) (Assessment, bool) {
	if chars >= ThinkStalled {
		return Assessment{State: Stalled, Score: 0.15, Reasons: []string{"rumination"},
			Evidence: ev("rumination", "%d think chars without action", chars)}, true
	}
	if chars >= ThinkUncertain {
		span := float64(ThinkStalled - ThinkUncertain)
		frac := float64(chars-ThinkUncertain) / span
		score := 0.4 - 0.2*frac
		return Assessment{State: Uncertain, Score: score, Reasons: []string{"rumination"},
			Evidence: ev("rumination", "%d think chars without action", chars)}, true
	}
	return Assessment{}, false
}

func (e *Engine) Assess(sig Signals) Assessment {
	actions := append(append([]string{}, e.actions...), sig.Actions...)
	errors := append(append([]string{}, e.errors...), sig.Errors...)
	exploratory := sig.Exploratory || anyExploratory(sig.Actions)
	early := (sig.Step > 0 && sig.Step <= earlyGrace) || (sig.Step == 0 && len(actions) < earlyGrace)
	distinct := distinctCount(actions)
	e.observeRepo(sig.RepoHash)
	e.observeEval(sig.EvalPassed, sig.EvalFingerprint)
	if sig.ThinkOnly {
		e.thinkOnlyRun++
	} else {
		e.thinkOnlyRun = 0
	}
	oscPath := e.observeWrites(sig.Writes)

	if n, ok := tailRepeat(actions); ok && n >= e.ActionThresh {
		fam := ""
		if len(actions) > 0 {
			fam = actions[len(actions)-1]
		}
		return Assessment{State: Looping, Score: 0.1, Reasons: []string{"repeated-action"}, Family: fam,
			Evidence: ev("repeated-action", "action %q repeated %d times", fam, n)}
	}
	if periodCycle(actions, 2, e.ActionThresh) && !tailAllExploratory(actions, 4) {
		return Assessment{State: Looping, Score: 0.1, Reasons: []string{"repeated-cycle"},
			Evidence: ev("repeated-cycle", "2-step action cycle repeated %d times", e.ActionThresh)}
	}
	if n, ok := tailRepeat(errors); ok && n >= e.ErrorThresh {
		return Assessment{State: Looping, Score: 0.1, Reasons: []string{"repeated-error"},
			Evidence: ev("repeated-error", "error %q repeated %d times", errors[len(errors)-1], n)}
	}
	if e.Regression && sig.PrevEvalPass != nil && sig.EvalPassed != nil && *sig.PrevEvalPass && !*sig.EvalPassed {
		return Assessment{State: Regressing, Score: 0.15, Reasons: []string{"evaluator-regression"},
			Evidence: ev("evaluator-regression", "evaluator flipped from pass to fail")}
	}
	if e.EditOscillation && oscPath != "" {
		return Assessment{State: Regressing, Score: 0.15, Reasons: []string{"edit-oscillation"},
			Evidence: ev("edit-oscillation", "%s written, changed, then reverted", oscPath)}
	}
	if e.TestStagnation > 0 && e.evalFailRun >= e.TestStagnation {
		return Assessment{State: Stalled, Score: 0.2, Reasons: []string{"test-stagnation"},
			Evidence: ev("test-stagnation", "same failing evaluation %d times in a row", e.evalFailRun)}
	}
	if sig.EvalPassed != nil && *sig.EvalPassed {
		return Assessment{State: Complete, Score: 1, Reasons: []string{"evaluator-passed"},
			Evidence: ev("evaluator-passed", "evaluator passed")}
	}
	if a, ok := thinkContent(e.thinks, sig.ThinkText); ok {
		return a
	}
	if e.RepoStagnation > 0 && e.repoSame >= e.RepoStagnation {
		return Assessment{State: Stalled, Score: 0.25, Reasons: []string{"repo-stagnation"},
			Evidence: ev("repo-stagnation", "repository state unchanged for %d assessments", e.repoSame)}
	}
	if sig.Meaningful {
		return Assessment{State: Progressing, Score: 0.7, Reasons: []string{"workspace-delta"},
			Evidence: ev("workspace-delta", "workspace diff present")}
	}
	// Plan-without-exec covers the sub-rumination zone: sustained
	// think-only runs that never reach the rumination char threshold keep
	// their rumination diagnosis below.
	if e.PlanWithoutExec > 0 && e.thinkOnlyRun >= e.PlanWithoutExec && sig.ThinkChars >= ThinkUncertain && sig.ThinkChars < ThinkStalled {
		return Assessment{State: Stalled, Score: 0.25, Reasons: []string{"plan-without-exec"},
			Evidence: ev("plan-without-exec", "%d think-only assessments (%d chars) without action", e.thinkOnlyRun, sig.ThinkChars)}
	}
	if sig.ThinkOnly {
		if a, ok := thinkAssessment(sig.ThinkChars); ok {
			return a
		}
		return Assessment{State: Progressing, Score: 0.5, Reasons: []string{"thinking"},
			Evidence: ev("thinking", "%d chars without tool calls", sig.ThinkChars)}
	}
	if exploratory {
		return Assessment{State: Progressing, Score: 0.65, Reasons: []string{"exploring"},
			Evidence: ev("exploring", "exploratory tool use")}
	}
	if early || (distinct < earlyGrace && sig.Step <= earlyGrace) {
		return Assessment{State: Progressing, Score: 0.55, Reasons: []string{"early-observation"},
			Evidence: ev("early-observation", "within grace window")}
	}
	if len(actions) < 2 {
		return Assessment{State: Progressing, Score: 0.5, Reasons: []string{"starting"},
			Evidence: ev("starting", "too few actions to judge")}
	}
	// Prefill (prompt tokens) is not progress failure. Only completion-token
	// burn after the grace window, with no workspace or explore evidence.
	if sig.CompletionTokens >= e.TokenBurn && sig.CompletionTokens > 0 {
		return Assessment{State: Stalled, Score: 0.2, Reasons: []string{"token-burn"},
			Evidence: ev("token-burn", "%d completion tokens without progress", sig.CompletionTokens)}
	}
	if e.CostBurnUSD > 0 && sig.CostUSD >= e.CostBurnUSD {
		return Assessment{State: Stalled, Score: 0.2, Reasons: []string{"cost-burn"},
			Evidence: ev("cost-burn", "$%.4f spent without progress", sig.CostUSD)}
	}
	if sig.StagnationN >= e.Stagnation {
		return Assessment{State: Stalled, Score: 0.25, Reasons: []string{"stagnation"},
			Evidence: ev("stagnation", "%d stagnant assessments", sig.StagnationN)}
	}
	return Assessment{State: Uncertain, Score: 0.4, Reasons: []string{"semantic-stagnation"},
		Evidence: ev("semantic-stagnation", "no progress evidence either way")}
}

func IsExploratory(name, args string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read", "search", "read_file", "search_files":
		return true
	case "git":
		return gitExplore(args)
	case "shell", "terminal":
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
	if cmd == "" {
		cmd = strings.TrimSpace(args)
	}
	cmd = stripWorkingDir(cmd)
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
	name = strings.ToLower(strings.TrimSpace(name))
	if fam := discoveryFamily(name, args); fam != "" {
		return fam
	}
	switch name {
	case "read_file", "read", "write_file", "write", "patch":
		if p := toolField(args, "path"); p != "" {
			return name + ":" + compact(p)
		}
	case "terminal", "shell":
		if c := toolField(args, "command"); c != "" {
			return name + ":" + compact(stripWorkingDir(c))
		}
	}
	return name + ":" + compact(args)
}

func discoveryFamily(name, args string) string {
	blob := strings.ToLower(name + " " + args)
	topic := discoveryTopic(blob)
	switch name {
	case "tool_search", "tool_describe", "tool_call", "skills_list", "skill_view":
		if topic != "" {
			return "discover:" + topic
		}
		return "discover"
	case "execute_code":
		if topic != "" {
			return "discover:" + topic
		}
	case "terminal", "shell":
		if strings.Contains(blob, "hermes mcp") || strings.Contains(blob, "hermes tools") ||
			strings.Contains(blob, "hermes auth") || strings.Contains(blob, "hermes login") {
			if topic != "" {
				return "discover:" + topic
			}
			return "discover:mcp"
		}
	}
	return ""
}

func discoveryTopic(blob string) string {
	for _, t := range []string{"linear", "blender", "github", "slack"} {
		if strings.Contains(blob, t) {
			return t
		}
	}
	if strings.Contains(blob, "mcp") {
		return "mcp"
	}
	return ""
}

func toolField(args, key string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func stripWorkingDir(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if i := strings.Index(cmd, "&&"); i >= 0 && strings.HasPrefix(strings.TrimSpace(cmd), "cd ") {
		return strings.TrimSpace(cmd[i+2:])
	}
	return cmd
}

func FingerprintError(s string) string {
	return Fingerprint(s)
}

// Fingerprint compacts text and returns a short stable hash for
// change detection across assessments.
func Fingerprint(s string) string {
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

func periodCycle(items []string, period, reps int) bool {
	if period < 2 || reps < 2 {
		return false
	}
	need := period * reps
	if len(items) < need {
		return false
	}
	tail := items[len(items)-need:]
	same := true
	for i := period; i < need; i++ {
		if tail[i] != tail[i-period] {
			return false
		}
		if tail[i] != tail[0] {
			same = false
		}
	}
	return !same
}

func tailAllExploratory(items []string, n int) bool {
	if len(items) < n {
		n = len(items)
	}
	if n == 0 {
		return false
	}
	for _, a := range items[len(items)-n:] {
		name, rest, _ := strings.Cut(a, ":")
		if !IsExploratory(name, rest) {
			return false
		}
	}
	return true
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
