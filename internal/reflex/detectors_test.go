package reflex

import (
	"strings"
	"testing"
)

func falsePtr() *bool { b := false; return &b }

func boolPtr(b bool) *bool { return &b }

// drive feeds signals in order and returns the final assessment. Like the
// run loop, it observes each step's actions into the engine afterwards.
func drive(e *Engine, sigs []Signals) Assessment {
	var a Assessment
	for _, s := range sigs {
		a = e.Assess(s)
		for _, n := range s.Actions {
			e.ObserveAction(n)
		}
		for _, fp := range s.Errors {
			e.ObserveError(fp)
		}
	}
	return a
}

func hasEvidence(a Assessment, detector string) bool {
	for _, e := range a.Evidence {
		if e.Detector == detector && strings.TrimSpace(e.Detail) != "" {
			return true
		}
	}
	return false
}

func TestNewDetectors(t *testing.T) {
	bigThink := strings.Repeat("plan ", 600) // 3000 chars
	cases := []struct {
		name   string
		tune   func(*Engine)
		sigs   []Signals
		state  ProgressState
		reason string
		negate bool // assert reason does NOT fire
	}{
		{
			name: "repo stagnation fires on unchanged state",
			sigs: []Signals{
				{Step: 10, RepoHash: "abc123"},
				{Step: 11, RepoHash: "abc123"},
				{Step: 12, RepoHash: "abc123"},
				{Step: 13, RepoHash: "abc123"},
			},
			state: Stalled, reason: "repo-stagnation",
		},
		{
			name: "repo stagnation ignores changing state",
			sigs: []Signals{
				{Step: 10, RepoHash: "a"},
				{Step: 11, RepoHash: "b"},
				{Step: 12, RepoHash: "c"},
				{Step: 13, RepoHash: "d"},
				{Step: 14, RepoHash: "e"},
			},
			state: Stalled, reason: "repo-stagnation", negate: true,
		},
		{
			name: "repo stagnation ignores empty hash",
			sigs: []Signals{
				{Step: 10}, {Step: 11}, {Step: 12}, {Step: 13}, {Step: 14},
			},
			state: Stalled, reason: "repo-stagnation", negate: true,
		},
		{
			name: "test stagnation fires on repeated identical failure",
			sigs: []Signals{
				{Step: 10, EvalPassed: falsePtr(), EvalFingerprint: "fp1"},
				{Step: 11, EvalPassed: falsePtr(), EvalFingerprint: "fp1"},
				{Step: 12, EvalPassed: falsePtr(), EvalFingerprint: "fp1"},
			},
			state: Stalled, reason: "test-stagnation",
		},
		{
			name: "test stagnation resets on pass",
			sigs: []Signals{
				{Step: 10, EvalPassed: falsePtr(), EvalFingerprint: "fp1"},
				{Step: 11, EvalPassed: falsePtr(), EvalFingerprint: "fp1"},
				{Step: 12, EvalPassed: boolPtr(true)},
				{Step: 13, EvalPassed: falsePtr(), EvalFingerprint: "fp1"},
				{Step: 14, EvalPassed: falsePtr(), EvalFingerprint: "fp1"},
			},
			state: Stalled, reason: "test-stagnation", negate: true,
		},
		{
			name: "cost burn fires over threshold",
			tune: func(e *Engine) {
				e.ApplyTuning(DetectorTuning{CostBurnUSD: 0.5})
			},
			sigs: []Signals{
				{Step: 10, CostUSD: 0.75, Actions: []string{"shell:a", "shell:b"}},
			},
			state: Stalled, reason: "cost-burn",
		},
		{
			name: "cost burn disabled by default",
			sigs: []Signals{
				{Step: 10, CostUSD: 999},
			},
			state: Stalled, reason: "cost-burn", negate: true,
		},
		{
			name: "plan without exec fires on repeated think-only",
			sigs: []Signals{
				{Step: 10, ThinkOnly: true, ThinkChars: 3000, ThinkText: bigThink},
				{Step: 11, ThinkOnly: true, ThinkChars: 3000, ThinkText: bigThink},
				{Step: 12, ThinkOnly: true, ThinkChars: 3000, ThinkText: bigThink},
			},
			state: Stalled, reason: "plan-without-exec",
		},
		{
			name: "plan without exec needs substantial thinking",
			sigs: []Signals{
				{Step: 10, ThinkOnly: true, ThinkChars: 100},
				{Step: 11, ThinkOnly: true, ThinkChars: 100},
				{Step: 12, ThinkOnly: true, ThinkChars: 100},
				{Step: 13, ThinkOnly: true, ThinkChars: 100},
			},
			state: Stalled, reason: "plan-without-exec", negate: true,
		},
		{
			name: "edit oscillation fires on revert",
			sigs: []Signals{
				{Step: 10, Writes: []FileWrite{{Path: "a.go", Hash: "h1"}}},
				{Step: 11, Writes: []FileWrite{{Path: "a.go", Hash: "h2"}}},
				{Step: 12, Writes: []FileWrite{{Path: "a.go", Hash: "h1"}}},
			},
			state: Regressing, reason: "edit-oscillation",
		},
		{
			name: "edit oscillation ignores steady progress",
			sigs: []Signals{
				{Step: 10, Writes: []FileWrite{{Path: "a.go", Hash: "h1"}}},
				{Step: 11, Writes: []FileWrite{{Path: "a.go", Hash: "h2"}}},
				{Step: 12, Writes: []FileWrite{{Path: "a.go", Hash: "h3"}}},
			},
			state: Regressing, reason: "edit-oscillation", negate: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngine(2, 3, 20000, 6, true)
			if tc.tune != nil {
				tc.tune(e)
			}
			got := drive(e, tc.sigs)
			if tc.negate {
				if hasReason(got, tc.reason) {
					t.Fatalf("reason %q fired unexpectedly: %+v", tc.reason, got)
				}
				return
			}
			if got.State != tc.state || !hasReason(got, tc.reason) {
				t.Fatalf("got %+v, want state %s reason %s", got, tc.state, tc.reason)
			}
			if got.Score <= 0 || got.Score > 1 {
				t.Fatalf("confidence out of range: %v", got.Score)
			}
			if !hasEvidence(got, tc.reason) {
				t.Fatalf("missing evidence trail: %+v", got.Evidence)
			}
		})
	}
}

func TestExistingDetectorsEmitEvidence(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	got := drive(e, []Signals{
		{Step: 10, Actions: []string{"shell:make"}},
		{Step: 11, Actions: []string{"shell:make"}},
	})
	if got.State != Looping || !hasReason(got, "repeated-action") {
		t.Fatalf("got %+v", got)
	}
	if !hasEvidence(got, "repeated-action") {
		t.Fatalf("missing evidence: %+v", got.Evidence)
	}

	e = NewEngine(2, 3, 20000, 6, true)
	got = e.Assess(Signals{Step: 10, CompletionTokens: 25000, Actions: []string{"shell:a", "shell:b"}})
	if got.State != Stalled || !hasReason(got, "token-burn") {
		t.Fatalf("got %+v", got)
	}
	if !hasEvidence(got, "token-burn") {
		t.Fatalf("missing evidence: %+v", got.Evidence)
	}
}

func TestDetectorTuningOverride(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	e.ApplyTuning(DetectorTuning{RepoStagnation: 2})
	got := drive(e, []Signals{
		{Step: 10, RepoHash: "z"},
		{Step: 11, RepoHash: "z"},
	})
	if got.State != Stalled || !hasReason(got, "repo-stagnation") {
		t.Fatalf("tuning not applied: %+v", got)
	}
}

func TestDetectorResetClearsNewState(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	drive(e, []Signals{
		{Step: 10, RepoHash: "z"},
		{Step: 11, RepoHash: "z"},
		{Step: 12, RepoHash: "z"},
	})
	e.Reset()
	got := e.Assess(Signals{Step: 13, RepoHash: "z"})
	if hasReason(got, "repo-stagnation") {
		t.Fatalf("stale repo counter survived reset: %+v", got)
	}
}
