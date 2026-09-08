package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
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

func tallSnap() run.Snapshot {
	evs := make([]event.Event, 40)
	for i := range evs {
		data, _ := json.Marshal(map[string]any{"tool": "shell", "args": fmt.Sprintf(`{"command":"step %d"}`, i)})
		evs[i] = event.Event{Sequence: uint64(i + 1), Type: event.ToolRequested, Data: data}
	}
	return run.Snapshot{Events: evs, Goal: "tall"}
}

func readyPane(t *testing.T, phase phase) Model {
	t.Helper()
	m := New(Options{})
	m.phase = phase
	m.ready = true
	m.follow = true
	m.vp = viewport.New(40, 5)
	m.refreshPane(tallSnap())
	return m
}

func TestPaneFollowsBottom(t *testing.T) {
	m := readyPane(t, phaseRunning)
	if !m.vp.AtBottom() {
		t.Fatal("follow should pin to latest")
	}
	if !m.follow {
		t.Fatal("follow on")
	}
}

func TestScrollUpUnpinsFollow(t *testing.T) {
	m := readyPane(t, phaseRunning)
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	got := next.(Model)
	if got.follow {
		t.Fatal("scroll up should unpin follow")
	}
	if got.vp.AtBottom() {
		t.Fatal("should not be at bottom")
	}
	got.refreshPane(tallSnap())
	if got.vp.AtBottom() {
		t.Fatal("refresh must not steal scroll")
	}
	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyEnd})
	got = next.(Model)
	if !got.follow || !got.vp.AtBottom() {
		t.Fatal("end should jump to latest and follow")
	}
}

func TestDoneArrowsScrollNotType(t *testing.T) {
	m := readyPane(t, phaseDone)
	m.input.SetValue("hello")
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	got := next.(Model)
	if got.input.Value() != "hello" {
		t.Fatalf("arrow should not edit reply: %q", got.input.Value())
	}
	if got.follow {
		t.Fatal("up should unpin")
	}
	next, _ = got.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	got = next.(Model)
	if !strings.Contains(got.input.Value(), "x") {
		t.Fatalf("letters still type: %q", got.input.Value())
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
