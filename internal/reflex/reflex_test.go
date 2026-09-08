package reflex

import "testing"

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

func TestParseAssessment(t *testing.T) {
	a, err := parseAssessment(`here {"state":"stalled","score":0.2,"reasons":["hard"]}`)
	if err != nil || a.State != Stalled {
		t.Fatalf("%+v %v", a, err)
	}
}
