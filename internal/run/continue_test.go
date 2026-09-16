package run

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/james-see/temper/internal/agent"
	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/store"
)

type fakeSide struct {
	mu        sync.Mutex
	id        string
	session   string
	polls     [][]agent.Ingest
	pollN     int
	injected  []string
	injectErr error
	caps      agent.Capabilities
}

func (f *fakeSide) ID() string { return f.id }
func (f *fakeSide) Capabilities(context.Context) (agent.Capabilities, error) {
	c := f.caps
	if !c.ToolCalls && !c.Resume && !c.ModelOverride {
		return agent.Capabilities{ToolCalls: true, Resume: true}, nil
	}
	return c, nil
}
func (f *fakeSide) Start(_ context.Context, req agent.TaskRequest) (agent.Session, error) {
	if req.Session != "" {
		f.session = req.Session
	}
	if f.session == "" {
		f.session = "ses-" + f.id
	}
	return agent.Session{ID: req.RunID}, nil
}
func (f *fakeSide) Resume(_ context.Context, id string) (agent.Session, error) {
	f.session = id
	return agent.Session{ID: id}, nil
}
func (f *fakeSide) Interrupt(context.Context, string) error { return nil }
func (f *fakeSide) Events(context.Context, string) (<-chan agent.Event, error) {
	return nil, fmt.Errorf("use Poll")
}
func (f *fakeSide) Poll(context.Context) ([]agent.Ingest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pollN < len(f.polls) {
		ings := f.polls[f.pollN]
		f.pollN++
		return ings, nil
	}
	return nil, nil
}
func (f *fakeSide) Inject(_ context.Context, prompt string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.injectErr != nil {
		return f.injectErr
	}
	f.injected = append(f.injected, prompt)
	return nil
}
func (f *fakeSide) SessionID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.session
}
func (f *fakeSide) LaunchLine() string { return f.id }
func (f *fakeSide) Bind(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.session = id
}

func TestContinueAPI(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Judge.Model = ""
	mgr, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	side := &fakeSide{id: "muse", session: "A"}
	side.polls = [][]agent.Ingest{
		{{Type: event.ModelCompleted, Data: map[string]any{"content": "hi"}}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan store.Run, 1)
	go func() {
		r, _ := mgr.Execute(ctx, Options{
			Goal: "ship", Root: dir, Agent: "muse", Sidecar: side, SkipSpawn: true, MaxSteps: 3,
		})
		done <- r
	}()
	waitEvent(t, mgr, event.SidecarBound, 3*time.Second)
	runID := mgr.Hub.Get().RunID
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	mgr2, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr2.Close()
	side2 := &fakeSide{id: "muse"}
	side2.polls = [][]agent.Ingest{
		{{Type: event.ModelCompleted, Data: map[string]any{"content": "again"}}},
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	go func() {
		_, _ = mgr2.ContinueWith(ctx2, runID, Options{Sidecar: side2, SkipSpawn: true, MaxSteps: 4})
	}()
	waitEvent(t, mgr2, event.RunStarted, 3*time.Second)
	if mgr2.Hub.Get().RunID != runID {
		t.Fatalf("want same run %s got %s", runID, mgr2.Hub.Get().RunID)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if side2.SessionID() == "A" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("want rebound session A got %s", side2.SessionID())
}

func TestEnqueuePromptAtTurnBoundary(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Judge.Model = ""
	mgr, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	side := &fakeSide{id: "goose"}
	side.polls = [][]agent.Ingest{
		{{Type: event.ToolRequested, Data: map[string]any{"tool": "shell"}}},
		{},
		{{Type: event.ToolCompleted, Data: map[string]any{"tool": "shell", "output": "ok"}}},
		{{Type: event.ModelCompleted, Data: map[string]any{"content": "ready"}}},
		{},
		{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_, _ = mgr.Execute(ctx, Options{
			Goal: "work", Root: dir, Agent: "goose", Sidecar: side, SkipSpawn: true, MaxSteps: 20,
		})
	}()
	waitEvent(t, mgr, event.AgentStarted, 3*time.Second)
	time.Sleep(50 * time.Millisecond)
	if err := mgr.EnqueuePrompt(ctx, "follow up please"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		side.mu.Lock()
		n := len(side.injected)
		side.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	side.mu.Lock()
	defer side.mu.Unlock()
	found := false
	for _, p := range side.injected {
		if strings.Contains(p, "follow up please") {
			found = true
		}
	}
	if !found {
		t.Fatalf("injected %v", side.injected)
	}
}

func TestSwitchAgentHandoff(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Judge.Model = ""
	cfg.Reflex.Mode = config.ReflexAuto
	cfg.Reflex.Detectors.ActionCycle.Repetitions = 2
	cfg.Reflex.Recovery = []config.RecoveryStep{
		{After: "first_stall", Action: "switch_agent"},
	}
	mgr, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	muse := &fakeSide{id: "muse", session: "m1"}
	goose := &fakeSide{id: "goose", session: "g1"}
	// Force looping via repeated identical tool actions.
	muse.polls = [][]agent.Ingest{
		{{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": "ls"}}, {Type: event.ToolCompleted, Data: map[string]any{"tool": "shell", "output": "x"}}},
		{{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": "ls"}}, {Type: event.ToolCompleted, Data: map[string]any{"tool": "shell", "output": "x"}}},
		{{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": "ls"}}, {Type: event.ToolCompleted, Data: map[string]any{"tool": "shell", "output": "x"}}},
		{{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": "ls"}}, {Type: event.ToolCompleted, Data: map[string]any{"tool": "shell", "output": "x"}}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	mgr.handoffSidecar = func(id string) agent.Sidecar {
		if id == "goose" || id == "hermes" || id == "opencode" {
			goose.id = id
			return goose
		}
		return &fakeSide{id: id, session: "x"}
	}

	go func() {
		_, _ = mgr.Execute(ctx, Options{
			Goal: "loop", Root: dir, Agent: "muse", Sidecar: muse, SkipSpawn: true, MaxSteps: 30,
		})
	}()
	waitEvent(t, mgr, event.AgentSwitched, 6*time.Second)
	var to string
	for _, ev := range mgr.Hub.Get().Events {
		if ev.Type == event.AgentSwitched {
			var d map[string]any
			_ = json.Unmarshal(ev.Data, &d)
			to = fmt.Sprint(d["to"])
		}
	}
	if to == "" || to == "muse" {
		t.Fatalf("expected handoff away from muse, got %q", to)
	}
	if goose.SessionID() == "" {
		t.Fatal("handoff sidecar never started")
	}
}

func TestPickHandoffAgent(t *testing.T) {
	cfg := config.Defaults()
	to, ok := pickHandoffAgent(cfg, "muse")
	if !ok || to == "muse" {
		t.Fatalf("%s %v", to, ok)
	}
}
