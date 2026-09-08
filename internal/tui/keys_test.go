package tui

import (
	"encoding/json"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/run"
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

func TestTabTogglesPane(t *testing.T) {
	m := New(Options{})
	m.phase = phaseRunning
	m.ready = true
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	got := next.(Model)
	if got.pane != paneIO {
		t.Fatal("tab -> io")
	}
	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	got = next.(Model)
	if got.pane != paneLog {
		t.Fatal("tab -> log")
	}
}

func TestDoneEnterFollowsUp(t *testing.T) {
	var got string
	m := New(Options{
		Provider: "ollama",
		Model:    "qwen",
		OnFollow: func(text string) { got = text },
	})
	m.phase = phaseDone
	m.goal = "old"
	m.input.SetValue("try curl instead")
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	mod := next.(Model)
	if mod.phase != phaseRunning {
		t.Fatalf("phase %d", mod.phase)
	}
	if got != "try curl instead" {
		t.Fatalf("follow %q", got)
	}
	if mod.goal != "old" {
		t.Fatal("keep thread goal")
	}
}

func TestDoneEmptyEnterStays(t *testing.T) {
	m := New(Options{Provider: "ollama", Model: "qwen"})
	m.phase = phaseDone
	m.goal = "old"
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)
	if got.phase != phaseDone {
		t.Fatalf("phase %d", got.phase)
	}
	if got.goal != "old" {
		t.Fatal("keep thread")
	}
}

func TestDonePrefixNNewGoal(t *testing.T) {
	m := New(Options{Provider: "ollama", Model: "qwen"})
	m.phase = phaseDone
	m.goal = "old"
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	got := next.(Model)
	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	got = next.(Model)
	if got.phase != phaseInput {
		t.Fatalf("phase %d", got.phase)
	}
	if got.goal != "" {
		t.Fatal("goal cleared")
	}
	if got.provider != "ollama" || got.model != "qwen" {
		t.Fatal("keep provider/model")
	}
}

func TestRenderModelIO(t *testing.T) {
	data, _ := json.Marshal(map[string]any{"content": "folder has a README", "tool_calls": 0})
	s := renderModelIO(run.Snapshot{
		Goal: "what is here",
		Events: []event.Event{{Type: event.ModelCompleted, Data: data}},
	}, 80)
	if !strings.Contains(s, "you") || !strings.Contains(s, "what is here") || !strings.Contains(s, "folder has a README") {
		t.Fatal(s)
	}
	follow, _ := json.Marshal(map[string]any{"text": "now list files"})
	s = renderModelIO(run.Snapshot{
		Goal: "what is here",
		Events: []event.Event{
			{Type: event.ModelCompleted, Data: data},
			{Type: event.UserMessage, Data: follow},
		},
	}, 80)
	if !strings.Contains(s, "now list files") {
		t.Fatal(s)
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
