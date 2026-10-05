package reflex

import (
	"testing"
	"time"
)

func TestLadderMaxTotal(t *testing.T) {
	l := NewLadderWithConfig(
		[]string{"replan", "critic", "switch_model"},
		LadderConfig{MaxEach: 1, MaxTotal: 2},
	)
	if _, ok := l.Next(); !ok {
		t.Fatal("first rung")
	}
	if _, ok := l.Next(); !ok {
		t.Fatal("second rung")
	}
	if _, ok := l.Next(); ok {
		t.Fatal("MaxTotal must exhaust the ladder")
	}
}

func TestLadderPerActionCaps(t *testing.T) {
	l := NewLadderWithConfig(
		[]string{"replan", "critic"},
		LadderConfig{MaxEach: 1, Caps: map[string]int{"replan": 2}},
	)
	if got, _ := l.Next(); got != "replan" {
		t.Fatalf("got %q", got)
	}
	if got, _ := l.Next(); got != "replan" {
		t.Fatalf("replan cap 2, got %q", got)
	}
	if got, _ := l.Next(); got != "critic" {
		t.Fatalf("got %q", got)
	}
	if _, ok := l.Next(); ok {
		t.Fatal("ladder must be exhausted")
	}
}

func TestLadderCooldown(t *testing.T) {
	now := time.Now()
	l := NewLadderWithConfig(
		[]string{"replan", "critic", "switch_model"},
		LadderConfig{MaxEach: 1, BackoffBase: time.Second, BackoffMax: 5 * time.Second},
	)
	l.now = func() time.Time { return now }
	if d := l.Cooldown(); d != 0 {
		t.Fatalf("fresh ladder must be ready, got %v", d)
	}
	if _, ok := l.Next(); !ok {
		t.Fatal("first rung")
	}
	if d := l.Cooldown(); d != time.Second {
		t.Fatalf("backoff 1s, got %v", d)
	}
	now = now.Add(1500 * time.Millisecond)
	if d := l.Cooldown(); d != 0 {
		t.Fatalf("cooldown must elapse, got %v", d)
	}
	if _, ok := l.Next(); !ok {
		t.Fatal("second rung")
	}
	if d := l.Cooldown(); d != 2*time.Second {
		t.Fatalf("backoff must double, got %v", d)
	}
	l.NoteProgress()
	if d := l.Cooldown(); d != 0 {
		t.Fatalf("progress must reset backoff, got %v", d)
	}
}

func TestLadderCooldownCap(t *testing.T) {
	now := time.Now()
	l := NewLadderWithConfig(
		[]string{"replan"},
		LadderConfig{MaxEach: 10, BackoffBase: time.Second, BackoffMax: 3 * time.Second},
	)
	l.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if _, ok := l.Next(); !ok {
			t.Fatal("rung")
		}
		now = now.Add(10 * time.Second)
	}
	if _, ok := l.Next(); !ok {
		t.Fatal("rung")
	}
	if d := l.Cooldown(); d != 3*time.Second {
		t.Fatalf("backoff must cap at max, got %v", d)
	}
}

func TestLadderNextForStampsAttempts(t *testing.T) {
	l := NewLadderWithConfig(
		[]string{"replan", "critic"},
		LadderConfig{MaxEach: 1, MaxTotal: 1},
	)
	a := Assessment{State: Looping, Reasons: []string{"repeated-action"}}
	if got, ok := l.NextFor(a); !ok || got != "replan" {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := l.NextFor(a); ok {
		t.Fatal("MaxTotal must apply to preferred picks too")
	}
}
