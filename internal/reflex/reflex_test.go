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
	e.ObserveAction("read:a")
	e.ObserveAction("write:b")
	a := e.Assess(Signals{})
	if a.State != Uncertain {
		t.Fatalf("want uncertain got %s", a.State)
	}
	ok := true
	a = e.Assess(Signals{EvalPassed: &ok})
	if a.State != Complete {
		t.Fatalf("want complete got %s", a.State)
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
