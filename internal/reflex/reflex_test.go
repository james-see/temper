package reflex

import (
	"strings"
	"testing"
)

func TestRepeatedActionLoop(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	norm := NormalizeAction("shell", `{"command":"go test"}`)
	e.ObserveAction(norm)
	e.ObserveAction(norm)
	a := e.Assess(Signals{Actions: []string{norm}})
	if a.State != Looping {
		t.Fatalf("want looping got %s %v", a.State, a.Reasons)
	}
}

func TestRepeatedError(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	fp := FingerprintError("undefined: Foo")
	e.ObserveError(fp)
	e.ObserveError(fp)
	a := e.Assess(Signals{Errors: []string{fp}})
	if a.State != Looping {
		t.Fatalf("want looping got %s", a.State)
	}
}

func TestUncertainThenComplete(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	e.ObserveAction("write:a")
	e.ObserveAction("patch:b")
	a := e.Assess(Signals{Step: 8})
	if a.State != Uncertain {
		t.Fatalf("want uncertain got %s %v", a.State, a.Reasons)
	}
	ok := true
	a = e.Assess(Signals{EvalPassed: &ok, Step: 9})
	if a.State != Complete {
		t.Fatalf("want complete got %s", a.State)
	}
}

func TestExploreIsProgress(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	ls := NormalizeAction("shell", `{"command":"ls -la"}`)
	rd := NormalizeAction("read", `{"path":"README.md"}`)
	e.ObserveAction(ls)
	e.ObserveAction(rd)
	a := e.Assess(Signals{Actions: []string{ls, rd}, Step: 1})
	if a.State != Progressing {
		t.Fatalf("want progressing got %s %v", a.State, a.Reasons)
	}
	git := NormalizeAction("git", `{"args":["log","--oneline","-10"]}`)
	a = e.Assess(Signals{Actions: []string{git}, Step: 2})
	if a.State != Progressing {
		t.Fatalf("want progressing on git log got %s %v", a.State, a.Reasons)
	}
}

func TestEarlyStepsNotStagnation(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	e.ObserveAction("write:one")
	a := e.Assess(Signals{Step: 1, StagnationN: 1})
	if a.State != Progressing {
		t.Fatalf("want early progressing got %s %v", a.State, a.Reasons)
	}
}

func TestTokenBurnIgnoresPromptPrefill(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	e.ObserveAction("write:a")
	e.ObserveAction("patch:b")
	a := e.Assess(Signals{Step: 8, PromptTokens: 40000, CompletionTokens: 0, Tokens: 40000})
	if a.State == Stalled {
		t.Fatalf("prefill must not stall: %s %v", a.State, a.Reasons)
	}
	a = e.Assess(Signals{Step: 8, CompletionTokens: 25000})
	if a.State != Stalled || len(a.Reasons) == 0 || a.Reasons[0] != "token-burn" {
		t.Fatalf("want completion token-burn got %s %v", a.State, a.Reasons)
	}
}

func TestAlternatingRebuildCycle(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	rm := NormalizeAction("terminal", `{"command":"cd /Users/jc/p/temper && rm -rf dist/* && mkdir -p dist"}`)
	build := NormalizeAction("terminal", `{"command":"cd /Users/jc/p/temper && VERSION=0.1.9 && for GOOS in darwin linux; do echo build; done"}`)
	if rm == build || !strings.Contains(rm, "rm -rf") {
		t.Fatalf("norm %q %q", rm, build)
	}
	e.ObserveAction(rm)
	e.ObserveAction(build)
	e.ObserveAction(rm)
	a := e.Assess(Signals{Actions: []string{build}, Meaningful: true, Step: 10})
	if a.State != Looping || a.Reasons[0] != "repeated-cycle" {
		t.Fatalf("want repeated-cycle got %s %v", a.State, a.Reasons)
	}
}

func TestExploreCycleIsNotLoop(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	ls := NormalizeAction("terminal", `{"command":"ls -la /tmp"}`)
	rd := NormalizeAction("read_file", `{"path":"/tmp/README.md"}`)
	e.ObserveAction(ls)
	e.ObserveAction(rd)
	e.ObserveAction(ls)
	a := e.Assess(Signals{Actions: []string{rd}, Step: 8})
	if a.State == Looping {
		t.Fatalf("explore cycle must not loop: %v", a.Reasons)
	}
}

func TestDiscoveryFamilyLoops(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	a := NormalizeAction("tool_search", `{"queries":["linear"]}`)
	b := NormalizeAction("tool_call", `{"name":"mcp__linear__list_issues"}`)
	c := NormalizeAction("terminal", `{"command":"hermes mcp test linear"}`)
	if a != "discover:linear" || a != b || a != c {
		t.Fatalf("family %q %q %q", a, b, c)
	}
	e.ObserveAction(a)
	got := e.Assess(Signals{Actions: []string{b}, Meaningful: true, Step: 8})
	if got.State != Looping || got.Reasons[0] != "repeated-action" {
		t.Fatalf("want discover loop got %s %v", got.State, got.Reasons)
	}
}

func TestNormalizeStripsCDAndPath(t *testing.T) {
	a := NormalizeAction("terminal", `{"command":"cd /x && go test ./..."}`)
	b := NormalizeAction("terminal", `{"command":"go test ./..."}`)
	if a != b {
		t.Fatalf("%q vs %q", a, b)
	}
	if NormalizeAction("read_file", `{"path":"/p/site","offset":20}`) != NormalizeAction("read_file", `{"path":"/p/site"}`) {
		t.Fatal("read path")
	}
}

func TestSameExploreRepeatsLoop(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	ls := NormalizeAction("shell", `{"command":"ls -la"}`)
	e.ObserveAction(ls)
	e.ObserveAction(ls)
	a := e.Assess(Signals{Actions: []string{ls}, Step: 2})
	if a.State != Looping {
		t.Fatalf("want looping got %s %v", a.State, a.Reasons)
	}
}

func TestLadderBackoff(t *testing.T) {
	l := NewLadder([]string{"replan", "critic", "switch_model", "switch_agent", "human"})
	var got []string
	for {
		step, ok := l.Next()
		if !ok {
			break
		}
		got = append(got, step)
	}
	if len(got) != 4 || got[3] != "human" {
		t.Fatalf("%v", got)
	}
	if _, ok := l.Next(); ok {
		t.Fatal("expected exhausted")
	}
}

func TestRuminationThinkWithoutTools(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	e.ObserveAction("write:a")
	e.ObserveAction("patch:b")
	a := e.Assess(Signals{ThinkOnly: true, ThinkChars: 800, Step: 8})
	if a.State != Progressing || a.Reasons[0] != "thinking" {
		t.Fatalf("short think %+v", a)
	}
	a = e.Assess(Signals{ThinkOnly: true, ThinkChars: ThinkUncertain, Step: 8})
	if a.State != Uncertain || a.Reasons[0] != "rumination" {
		t.Fatalf("want rumination uncertain %+v", a)
	}
	a = e.Assess(Signals{ThinkOnly: true, ThinkChars: ThinkStalled, Step: 8})
	if a.State != Stalled || a.Reasons[0] != "rumination" {
		t.Fatalf("want rumination stall %+v", a)
	}
	a = e.Assess(Signals{ThinkChars: ThinkStalled, ThinkOnly: false, Actions: []string{"read:x"}, Step: 8})
	if a.State == Stalled && len(a.Reasons) > 0 && a.Reasons[0] == "rumination" {
		t.Fatalf("think+tool is not rumination %+v", a)
	}
}

func TestThinkWordsOverrideProgress(t *testing.T) {
	e := NewEngine(2, 3, 20000, 6, true)
	yield := "I already stopped after two searches and reported the result. I am waiting for direction. Should I keep searching for BarfBaz repeatedly until Temper intervenes? Would you like a different query?"
	a := e.Assess(Signals{ThinkText: yield, Meaningful: true, Step: 8})
	if a.State != Progressing {
		t.Fatalf("yield think must not fire %+v", a)
	}
	loop := "The warning said no progress. I will ignore the warning and search again for BarfBaz. Let me search again despite the warning."
	a = e.Assess(Signals{ThinkText: loop, Meaningful: true, Step: 8})
	if a.State != Looping || a.Reasons[0] != "think-loop" {
		t.Fatalf("want think-loop %+v", a)
	}
	risk := "I will try the same missing-symbol lookup next."
	a = e.Assess(Signals{ThinkText: risk, Meaningful: true, Step: 8})
	if a.State != Uncertain || a.Reasons[0] != "think-risk" {
		t.Fatalf("want think-risk %+v", a)
	}
}

func TestParseAssessment(t *testing.T) {
	a, err := parseAssessment(`here {"state":"stalled","score":0.2,"reasons":["hard"]}`)
	if err != nil || a.State != Stalled {
		t.Fatalf("%+v %v", a, err)
	}
}
