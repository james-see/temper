package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPrefixCommand(t *testing.T) {
	if !isPrefixKey("ctrl+b") {
		t.Fatal("ctrl+b")
	}
	cmd, ok := prefixCommand("s")
	if !ok || cmd != "status" {
		t.Fatalf("%s %v", cmd, ok)
	}
	if !typingPhase(phaseInput) {
		t.Fatal("input is typing")
	}
	if typingPhase(phaseRunning) {
		t.Fatal("running is not typing")
	}
}

func TestInputKeysNotCommands(t *testing.T) {
	m := New(Options{})
	m.phase = phaseInput
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	got := next.(Model)
	if got.overlay != overlayNone {
		t.Fatal("s should type, not open status")
	}
	if got.phase != phaseInput {
		t.Fatal("still input")
	}
	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	got = next.(Model)
	if !got.prefix {
		t.Fatal("prefix pending")
	}
	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	got = next.(Model)
	if got.prefix {
		t.Fatal("prefix consumed")
	}
	if got.overlay != overlayStatus {
		t.Fatal("ctrl+b s opens status")
	}
}

func TestPickerJK(t *testing.T) {
	m := New(Options{})
	m.phase = phasePickProvider
	m.items = []pickItem{{ID: "a", Title: "a", Usable: true}, {ID: "b", Title: "b", Usable: true}}
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	got := next.(Model)
	if got.cursor != 1 {
		t.Fatalf("cursor %d", got.cursor)
	}
}
