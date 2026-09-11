package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/james-see/temper/internal/agent"
)

func TestSessionPickerItemsGrouped(t *testing.T) {
	items := sessionPickerItems([]agent.SessionPick{
		{Agent: "cursor", SessionID: "c1", Label: "temper"},
		{Agent: "hermes", SessionID: "h1", Label: "auth"},
		{Agent: "opencode", SessionID: "ses_1", Label: "fix"},
	}, "", true)
	if items[0].Kind != kindAttachAll {
		t.Fatalf("%+v", items[0])
	}
	var kinds []string
	for _, it := range items {
		kinds = append(kinds, it.Kind+":"+it.Title)
	}
	joined := strings.Join(kinds, ",")
	if !strings.Contains(joined, kindHeader) || !strings.Contains(joined, "all cursor") {
		t.Fatal(joined)
	}
	if items[len(items)-1].Kind != kindNative {
		t.Fatalf("want native escape %+v", items[len(items)-1])
	}
}

func TestHermesSpawnsWhenNoSessions(t *testing.T) {
	started := false
	var got AttachSpec
	m := New(Options{
		Agent: "hermes",
		OnStart: func(_, _, _ string, attach AttachSpec) {
			started = true
			got = attach
		},
		ListSessions: func(string) ([]agent.SessionPick, error) { return nil, nil },
	})
	next, cmd := m.Update(splashDone{})
	gotM := next.(Model)
	if gotM.phase != phasePickSession {
		t.Fatalf("phase %d", gotM.phase)
	}
	if cmd == nil {
		t.Fatal("expected fetch cmd")
	}
	next, _ = gotM.Update(sessionPicksMsg{})
	gotM = next.(Model)
	if gotM.phase != phaseRunning || !started || !got.Spawn {
		t.Fatalf("phase %d started %v attach %+v", gotM.phase, started, got)
	}
}

func TestUnifiedPickerNativeEscape(t *testing.T) {
	m := New(Options{
		ListSessions: func(string) ([]agent.SessionPick, error) {
			return []agent.SessionPick{
				{Agent: "cursor", SessionID: "a", Label: "temper"},
				{Agent: "hermes", SessionID: "b", Label: "auth"},
			}, nil
		},
	})
	next, _ := m.Update(splashDone{})
	got := next.(Model)
	next, _ = got.Update(sessionPicksMsg{Picks: []agent.SessionPick{
		{Agent: "cursor", SessionID: "a", Label: "temper"},
		{Agent: "hermes", SessionID: "b", Label: "auth"},
	}})
	got = next.(Model)
	if got.phase != phasePickSession {
		t.Fatalf("phase %d", got.phase)
	}
	// Move to native run row (last)
	got.cursor = len(got.items) - 1
	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(Model)
	if got.phase != phasePickProvider && got.phase != phasePickModel && got.phase != phaseInput && got.phase != phaseRunning {
		// With usable catalog empty, should be provider picker or model
		if got.external {
			t.Fatalf("should leave external path phase=%d", got.phase)
		}
	}
}

func TestMultiSelectSpaceAndEnter(t *testing.T) {
	var got AttachSpec
	m := New(Options{
		Agent: "opencode",
		OnStart: func(_, _, _ string, attach AttachSpec) { got = attach },
	})
	next, _ := m.Update(sessionPicksMsg{Picks: []agent.SessionPick{
		{Agent: "opencode", SessionID: "ses_a", Label: "a"},
		{Agent: "opencode", SessionID: "ses_b", Label: "b"},
	}})
	gotM := next.(Model)
	// Find first session row and toggle two sessions.
	var sessionIdx []int
	for i, it := range gotM.items {
		if it.Kind == kindSession {
			sessionIdx = append(sessionIdx, i)
		}
	}
	if len(sessionIdx) < 2 {
		t.Fatalf("items %+v", gotM.items)
	}
	gotM.cursor = sessionIdx[0]
	next, _ = gotM.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	gotM = next.(Model)
	gotM.cursor = sessionIdx[1]
	next, _ = gotM.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	gotM = next.(Model)
	next, _ = gotM.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	gotM = next.(Model)
	if gotM.phase != phaseRunning || len(got.Targets) != 2 {
		t.Fatalf("phase %d targets %+v", gotM.phase, got.Targets)
	}
}

func TestAttachAllEnter(t *testing.T) {
	var got AttachSpec
	m := New(Options{
		Agent:   "cursor",
		OnStart: func(_, _, _ string, attach AttachSpec) { got = attach },
	})
	next, _ := m.Update(sessionPicksMsg{Picks: []agent.SessionPick{
		{Agent: "cursor", SessionID: "a", Workspace: "/t", Label: "temper"},
		{Agent: "cursor", SessionID: "b", Workspace: "/v", Label: "vala"},
	}})
	gotM := next.(Model)
	gotM.cursor = 0 // attach all
	next, _ = gotM.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	gotM = next.(Model)
	if gotM.phase != phaseRunning || len(got.Targets) != 2 {
		t.Fatalf("phase %d %+v", gotM.phase, got.Targets)
	}
}

func TestDigitJumpsWithoutConfirm(t *testing.T) {
	started := false
	m := New(Options{
		Agent:   "cursor",
		OnStart: func(_, _, _ string, _ AttachSpec) { started = true },
	})
	next, _ := m.Update(sessionPicksMsg{Picks: []agent.SessionPick{
		{Agent: "cursor", SessionID: "a", Label: "temper"},
		{Agent: "cursor", SessionID: "b", Label: "vala"},
	}})
	got := next.(Model)
	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	got = next.(Model)
	if started || got.phase != phasePickSession {
		t.Fatalf("digit should only move focus; started=%v phase=%d", started, got.phase)
	}
}
