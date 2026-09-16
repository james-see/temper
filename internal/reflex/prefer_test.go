package reflex

import "testing"

func TestPreferRecoveryDiscover(t *testing.T) {
	got := PreferRecovery(Assessment{State: Looping, Family: "discover:linear", Reasons: []string{"repeated-action"}})
	if len(got) == 0 || got[0] != "human" {
		t.Fatalf("%v", got)
	}
}

func TestPreferRecoveryRumination(t *testing.T) {
	got := PreferRecovery(Assessment{State: Stalled, Reasons: []string{"rumination"}})
	if len(got) == 0 || got[0] != "switch_model" {
		t.Fatalf("%v", got)
	}
}

func TestPreferRecoveryRepeatedError(t *testing.T) {
	got := PreferRecovery(Assessment{State: Looping, Reasons: []string{"repeated-error"}})
	if len(got) == 0 || got[0] != "critic" {
		t.Fatalf("%v", got)
	}
}

func TestPreferRecoveryDefaultEmpty(t *testing.T) {
	got := PreferRecovery(Assessment{State: Uncertain, Reasons: []string{"starting"}})
	if got != nil {
		t.Fatalf("%v", got)
	}
}

func TestLadderNextForPrefersThenFallsThrough(t *testing.T) {
	l := NewLadder([]string{"replan", "critic", "switch_model", "human"})
	a := Assessment{State: Looping, Family: "discover:slack", Reasons: []string{"repeated-action"}}
	step, ok := l.NextFor(a)
	if !ok || step != "human" {
		t.Fatalf("first %s %v", step, ok)
	}
	// human exhausted; preferred critic next
	step, ok = l.NextFor(a)
	if !ok || step != "critic" {
		t.Fatalf("second %s %v", step, ok)
	}
	step, ok = l.NextFor(a)
	if !ok || step != "replan" {
		t.Fatalf("third %s %v", step, ok)
	}
}

func TestLadderNextForRuminationUsesSwitchModel(t *testing.T) {
	l := NewLadder([]string{"replan", "critic", "switch_model", "human"})
	a := Assessment{State: Stalled, Reasons: []string{"rumination"}}
	step, ok := l.NextFor(a)
	if !ok || step != "switch_model" {
		t.Fatalf("%s %v", step, ok)
	}
}
