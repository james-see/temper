package run

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/james-see/temper/internal/agent"
	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/term"
)

func fakeHermes(t *testing.T) *agent.Hermes {
	t.Helper()
	h := agent.NewHermes("hermes", false)
	h.LookPath = func(string) (string, error) { return "/bin/hermes", nil }
	h.OpenTerm = func([]string, string) (*term.Handle, error) { return &term.Handle{}, nil }
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

func TestExternalLiveOwnerHolds(t *testing.T) {
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
	resumes := 0
	base := h.Exec
	h.Exec = func(ctx context.Context, name string, args []string) (string, error) {
		if contains(args, "--resume") {
			resumes++
			return "", fmt.Errorf("%s: exit status 1: Session x already has a live owner (tui, pid 1, running 1m)", name)
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
	waitEvent(t, mgr, event.RecoveryFailed, 4*time.Second)
	time.Sleep(1200 * time.Millisecond)
	if resumes != 1 {
		t.Fatalf("live-owner retries %d want 1", resumes)
	}
	if mgr.Hub.Get().PendingRecovery == "" {
		t.Fatal("expected held pending")
	}
	cancel()
	<-done
}

func TestSidecarSignals(t *testing.T) {
	actions, errs, meaningful, think, only, preview := sidecarSignals([]agent.Ingest{
		{Type: event.ToolRequested, Data: map[string]any{"tool": "read_file", "args": `{"path":"a.go"}`}},
		{Type: event.ToolCompleted, Data: map[string]any{"tool": "read_file", "output": "ok"}},
	})
	if !meaningful || len(actions) != 1 || len(errs) != 0 || think != 0 || only || preview != "" {
		t.Fatalf("%v %v %v think=%d only=%v preview=%q", meaningful, actions, errs, think, only, preview)
	}
	_, _, meaningful, _, _, _ = sidecarSignals([]agent.Ingest{
		{Type: event.ToolCompleted, Data: map[string]any{"tool": "read_file", "output": `{"status": "unchanged"}`}},
	})
	if meaningful {
		t.Fatal("unchanged re-read is not progress")
	}
	_, _, meaningful, think, only, _ = sidecarSignals([]agent.Ingest{
		{Type: event.ModelThinking, Data: map[string]any{"chars": 4000, "delta": 4000, "tool_calls": 0, "text": false}},
	})
	if meaningful || think != 4000 || !only {
		t.Fatalf("think-only meaningful=%v think=%d only=%v", meaningful, think, only)
	}
	_, _, _, think, only, preview = sidecarSignals([]agent.Ingest{
		{Type: event.ModelThinking, Data: map[string]any{"delta": 8000, "preview": "let me search again"}},
		{Type: event.ToolRequested, Data: map[string]any{"tool": "shell", "args": `{"command":"ls"}`}},
	})
	if think != 8000 || only || preview != "let me search again" {
		t.Fatalf("think+tool think=%d only=%v preview=%q", think, only, preview)
	}
}

func TestExternalIdleDoesNotStall(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Mode = config.ReflexAuto
	cfg.Reflex.Judge.Model = ""
	cfg.Reflex.Detectors.Stagnation.Actions = 2
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
	time.Sleep(1500 * time.Millisecond)
	for _, ev := range mgr.Hub.Get().Events {
		if ev.Type == event.StagnationDetected || ev.Type == event.RecoveryStarted {
			t.Fatalf("idle sidecar fired %s", ev.Type)
		}
	}
	cancel()
	<-done
}

func TestSidecarClassifiesUserTurns(t *testing.T) {
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
		_, _ = mgr.Execute(ctx, Options{Root: dir, Agent: "hermes", SkipSpawn: true, Hermes: h, Goal: "list the files here"})
	}()
	waitEvent(t, mgr, event.AgentStarted, 3*time.Second)
	h.Queue(agent.Ingest{Type: event.UserMessage, Data: map[string]any{"text": "can we run a speedtest cli please"}})
	waitEvent(t, mgr, event.GoalShifted, 3*time.Second)
	if got := mgr.Hub.Get().ActiveGoal; got != "can we run a speedtest cli please" {
		t.Fatalf("active %q", got)
	}
	h.Queue(agent.Ingest{Type: event.UserMessage, Data: map[string]any{"text": "why so slow"}})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.sess != nil && mgr.sess.opts.Goal == "can we run a speedtest cli please" {
			var users int
			for _, ev := range mgr.Hub.Get().Events {
				if ev.Type == event.UserMessage {
					users++
				}
			}
			if users >= 2 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if mgr.sess.opts.Goal != "can we run a speedtest cli please" {
		t.Fatalf("follow-up shifted goal to %q", mgr.sess.opts.Goal)
	}
	shifts, evals := 0, 0
	for _, ev := range mgr.Hub.Get().Events {
		if ev.Type == event.GoalShifted {
			shifts++
		}
		if ev.Type == event.ProgressEvaluated {
			evals++
		}
	}
	if shifts != 1 {
		t.Fatalf("goal.shifted count %d", shifts)
	}
	if evals != 0 {
		t.Fatal("user-only turn should not emit progressing")
	}
	cancel()
	<-done
}

func TestSidecarDiscoveryNoop(t *testing.T) {
	if !sidecarNoop(agent.Ingest{Type: event.ToolCompleted, Data: map[string]any{
		"tool": "tool_search", "output": `{"queries":["linear"],"results":[{"matches":[]}]}`,
	}}) {
		t.Fatal("empty tool_search")
	}
	if !sidecarNoop(agent.Ingest{Type: event.ToolCompleted, Data: map[string]any{
		"tool": "tool_call", "error": "'mcp__linear__list_issues' is not available in this session",
		"output": `{"error":"not available"}`,
	}}) {
		t.Fatal("missing linear tool")
	}
	if !discoveryMiss("oauth error: non-interactive environment and no cached tokens found") {
		t.Fatal("oauth miss")
	}
	if systemNoise("[System: The active model for this chat has changed to glm-5.3-flash]") {
		// ok
	} else {
		t.Fatal("system noise")
	}
}

func TestResolveCursorTargets(t *testing.T) {
	got, err := resolveCursorTargets(Options{CursorTargets: []agent.CursorPick{{SessionID: "a", Workspace: "/t"}}})
	if err != nil || len(got) != 1 || got[0].SessionID != "a" {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = resolveCursorTargets(Options{CursorSession: "abc", CursorWorkspace: "/y"})
	if err != nil || len(got) != 1 || got[0].Workspace != "/y" {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = resolveCursorTargets(Options{})
	if err != nil || got != nil {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestResolveAttachTargetsHermesSession(t *testing.T) {
	got, err := resolveAttachTargets(Options{
		Agent: "hermes", CursorSession: "20260908_120000_bbbbbb", CursorWorkspace: "/ws",
	}, "hermes")
	if err != nil || len(got) != 1 || got[0].Agent != "hermes" || got[0].SessionID != "20260908_120000_bbbbbb" {
		t.Fatalf("%+v %v", got, err)
	}
	direct, err := resolveAttachTargets(Options{AttachTargets: []agent.SessionPick{
		{Agent: "opencode", SessionID: "ses_1"},
		{Agent: "hermes", SessionID: "h1"},
	}}, "")
	if err != nil || len(direct) != 2 {
		t.Fatalf("%+v %v", direct, err)
	}
}

func TestDescribeSidecarsMux(t *testing.T) {
	a := &stubBound{id: "cursor", session: "aaa", ws: "/t"}
	b := &stubBound{id: "hermes", session: "bbb", ws: "/v"}
	list := describeSidecars(agent.NewMux("control", []agent.Sidecar{a, b}), "mixed")
	if len(list) != 2 || list[0].Session != "aaa" || list[1].Agent != "hermes" || !list[0].Focus || list[1].Focus {
		t.Fatalf("%+v", list)
	}
}

type stubBound struct {
	id, session, ws string
}

func (s *stubBound) ID() string { return s.id }
func (s *stubBound) Capabilities(context.Context) (agent.Capabilities, error) {
	return agent.Capabilities{}, nil
}
func (s *stubBound) Start(context.Context, agent.TaskRequest) (agent.Session, error) {
	return agent.Session{ID: s.session}, nil
}
func (s *stubBound) Resume(context.Context, string) (agent.Session, error) {
	return agent.Session{ID: s.session}, nil
}
func (s *stubBound) Interrupt(context.Context, string) error { return nil }
func (s *stubBound) Events(context.Context, string) (<-chan agent.Event, error) {
	return nil, nil
}
func (s *stubBound) Poll(context.Context) ([]agent.Ingest, error) { return nil, nil }
func (s *stubBound) Inject(context.Context, string) error         { return nil }
func (s *stubBound) SessionID() string                            { return s.session }
func (s *stubBound) LaunchLine() string                           { return s.id }

func TestSidecarUserOnly(t *testing.T) {
	if !sidecarUserOnly([]agent.Ingest{{Type: event.UserMessage, Data: map[string]any{"text": "hi"}}}) {
		t.Fatal("user only")
	}
	if sidecarUserOnly([]agent.Ingest{
		{Type: event.UserMessage, Data: map[string]any{"text": "hi"}},
		{Type: event.ToolRequested, Data: map[string]any{"tool": "shell"}},
	}) {
		t.Fatal("mixed")
	}
}
