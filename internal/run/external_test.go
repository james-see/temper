package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/james-see/temper/internal/agent"
	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/event"
)

func fakeHermes(t *testing.T) *agent.Hermes {
	t.Helper()
	h := agent.NewHermes("hermes", false)
	h.LookPath = func(string) (string, error) { return "/bin/hermes", nil }
	h.OpenTerm = func([]string, string) error { return nil }
	listN := 0
	h.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		switch {
		case len(args) > 0 && args[0] == "--version":
			return "hermes 1", nil
		case contains(args, "sessions") && contains(args, "list"):
			listN++
			if listN == 1 {
				return "old 20260901_000000_aaaaaa\n", nil
			}
			return "old 20260901_000000_aaaaaa\nnew 20260908_120000_bbbbbb\n", nil
		case contains(args, "export"):
			return "", nil
		case contains(args, "logs"):
			return "", nil
		case contains(args, "--resume"):
			return "ok", nil
		default:
			return "", nil
		}
	}
	return h
}

func contains(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func waitEvent(t *testing.T, mgr *Manager, typ string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, ev := range mgr.Hub.Get().Events {
			if ev.Type == typ {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("missing %s in %s", typ, types(mgr.Hub.Get().Events))
}

func TestExternalSkipsProvider(t *testing.T) {
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
	h := fakeHermes(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = mgr.Execute(ctx, Options{Root: dir, Agent: "hermes", SkipSpawn: true, Hermes: h, MaxSteps: 4})
	}()
	waitEvent(t, mgr, event.AgentStarted, 3*time.Second)
	snap := mgr.Hub.Get()
	if snap.Provider != "" || snap.Model != "" {
		t.Fatalf("provider %q model %q", snap.Provider, snap.Model)
	}
	if snap.Agent != "hermes" {
		t.Fatalf("agent %s", snap.Agent)
	}
	if snap.Workspace != dir {
		t.Fatalf("workspace %s want %s", snap.Workspace, dir)
	}
	cancel()
	<-done
}

func TestExternalAutoInjects(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Mode = config.ReflexAuto
	cfg.Reflex.Judge.Model = ""
	cfg.Reflex.Detectors.ActionCycle.Repetitions = 2
	mgr, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	h := fakeHermes(t)
	var injected []string
	base := h.Exec
	h.Exec = func(ctx context.Context, name string, args []string) (string, error) {
		if contains(args, "--resume") && contains(args, "-q") {
			for i, a := range args {
				if a == "-q" && i+1 < len(args) {
					injected = append(injected, args[i+1])
				}
			}
		}
		return base(ctx, name, args)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = mgr.Execute(ctx, Options{Root: dir, Agent: "hermes", SkipSpawn: true, Hermes: h})
	}()
	waitEvent(t, mgr, event.AgentStarted, 3*time.Second)
	same := `{"command":"echo same"}`
	h.Queue(
		agent.Ingest{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": same}},
		agent.Ingest{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": same}},
	)
	waitEvent(t, mgr, event.RecoveryCompleted, 4*time.Second)
	if len(injected) == 0 {
		t.Fatal("expected inject")
	}
	if !strings.Contains(injected[0], "replan") && !strings.Contains(injected[0], "Reflex") {
		t.Fatalf("prompt %q", injected[0])
	}
	cancel()
	<-done
}

func TestExternalHumanDoesNotInject(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Mode = config.ReflexHuman
	cfg.Reflex.Judge.Model = ""
	cfg.Reflex.Detectors.ActionCycle.Repetitions = 2
	mgr, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	h := fakeHermes(t)
	injected := 0
	base := h.Exec
	h.Exec = func(ctx context.Context, name string, args []string) (string, error) {
		if contains(args, "--resume") {
			injected++
		}
		return base(ctx, name, args)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = mgr.Execute(ctx, Options{Root: dir, Agent: "hermes", SkipSpawn: true, Hermes: h})
	}()
	waitEvent(t, mgr, event.AgentStarted, 3*time.Second)
	same := `{"command":"echo same"}`
	h.Queue(
		agent.Ingest{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": same}},
		agent.Ingest{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": same}},
	)
	waitEvent(t, mgr, event.RecoveryStarted, 4*time.Second)
	time.Sleep(300 * time.Millisecond)
	if injected != 0 {
		t.Fatalf("human mode injected %d", injected)
	}
	if mgr.Hub.Get().PendingRecovery == "" {
		t.Fatal("expected pending")
	}
	mgr.ApproveRecovery()
	waitEvent(t, mgr, event.RecoveryCompleted, 3*time.Second)
	if injected == 0 {
		t.Fatal("approve should inject")
	}
	cancel()
	<-done
}

func TestToggleModeAppliesPending(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Mode = config.ReflexHuman
	cfg.Reflex.Judge.Model = ""
	cfg.Reflex.Detectors.ActionCycle.Repetitions = 2
	mgr, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	h := fakeHermes(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = mgr.Execute(ctx, Options{Root: dir, Agent: "hermes", SkipSpawn: true, Hermes: h})
	}()
	waitEvent(t, mgr, event.AgentStarted, 3*time.Second)
	same := `{"command":"echo same"}`
	h.Queue(
		agent.Ingest{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": same}},
		agent.Ingest{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": same}},
	)
	waitEvent(t, mgr, event.RecoveryStarted, 4*time.Second)
	if mgr.ToggleReflexMode() != config.ReflexAuto {
		t.Fatal("toggle auto")
	}
	waitEvent(t, mgr, event.RecoveryCompleted, 3*time.Second)
	cancel()
	<-done
}
