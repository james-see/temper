package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/provider"
)

func TestExecuteDetectsLoop(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Detectors.ActionCycle.Repetitions = 2
	cfg.Reflex.Judge.Model = ""
	mgr, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	n := 0
	mock := &provider.Mock{
		Name: "mock",
		Handler: func(provider.Request) provider.Result {
			n++
			if n > 6 {
				return provider.Result{Content: "done"}
			}
			return provider.Result{
				ToolCalls: []provider.ToolCall{{
					ID: "1", Name: "shell", Arguments: `{"command":"echo same"}`,
				}},
				Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 5},
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r, err := mgr.Execute(ctx, Options{Goal: "loop me", Root: dir, Prov: mock, MaxSteps: 10})
	if r.ID == "" {
		t.Fatal("no run id")
	}
	evs, err2 := mgr.Store.ListEvents(ctx, r.ID)
	if err2 != nil {
		t.Fatal(err2)
	}
	var sawLoop, sawRecover bool
	for _, ev := range evs {
		if ev.Type == event.LoopDetected {
			sawLoop = true
		}
		if ev.Type == event.RecoveryStarted {
			sawRecover = true
		}
	}
	if !sawLoop {
		t.Fatalf("expected loop.detected; err=%v events=%s", err, types(evs))
	}
	if !sawRecover {
		t.Fatalf("expected recovery; events=%s", types(evs))
	}
}

func TestExploreThenAnswerCompletes(t *testing.T) {
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

	n := 0
	mock := &provider.Mock{
		Name: "mock",
		Handler: func(provider.Request) provider.Result {
			n++
			switch n {
			case 1:
				return provider.Result{
					ToolCalls: []provider.ToolCall{
						{ID: "1", Name: "shell", Arguments: `{"command":"ls -la"}`},
						{ID: "2", Name: "shell", Arguments: `{"command":"pwd"}`},
					},
					Usage: provider.Usage{PromptTokens: 664, CompletionTokens: 120},
				}
			case 2:
				return provider.Result{
					ToolCalls: []provider.ToolCall{
						{ID: "3", Name: "read", Arguments: `{"path":"README"}`},
						{ID: "4", Name: "git", Arguments: `{"args":["log","--oneline","-10"]}`},
					},
					Usage: provider.Usage{PromptTokens: 1110, CompletionTokens: 142},
				}
			default:
				return provider.Result{
					Content: "Go repo with a README. Working tree clean.",
					Usage:   provider.Usage{PromptTokens: 3116, CompletionTokens: 587},
				}
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r, err := mgr.Execute(ctx, Options{Goal: "check on the current folder", Root: dir, Prov: mock, MaxSteps: 10})
	if err != nil {
		t.Fatalf("explore run failed: %v", err)
	}
	if r.State != StateCompleted {
		t.Fatalf("want completed got %s", r.State)
	}
	evs, err := mgr.Store.ListEvents(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Type == event.RunFailed {
			t.Fatalf("explore must not fail: %s", types(evs))
		}
	}
}

func TestPrefillDoesNotFailRun(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Judge.Model = ""
	cfg.Reflex.Detectors.TokenBurn.Threshold = 20000
	mgr, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	n := 0
	mock := &provider.Mock{
		Name: "mock",
		Handler: func(provider.Request) provider.Result {
			n++
			if n == 1 {
				return provider.Result{
					ToolCalls: []provider.ToolCall{{ID: "1", Name: "read", Arguments: `{"path":"README"}`}},
					Usage:     provider.Usage{PromptTokens: 39741, CompletionTokens: 80},
				}
			}
			return provider.Result{
				Content: "readme is short",
				Usage:   provider.Usage{PromptTokens: 40000, CompletionTokens: 40},
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r, err := mgr.Execute(ctx, Options{Goal: "read the readme", Root: dir, Prov: mock, MaxSteps: 6})
	if err != nil {
		t.Fatalf("prefill run failed: %v", err)
	}
	if r.State != StateCompleted {
		t.Fatalf("want completed got %s err=%v", r.State, err)
	}
}

func TestFollowupContinuesSession(t *testing.T) {
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

	n := 0
	var lastUser string
	mock := &provider.Mock{
		Name: "mock",
		Handler: func(req provider.Request) provider.Result {
			n++
			if len(req.Messages) > 0 {
				last := req.Messages[len(req.Messages)-1]
				if last.Role == "user" {
					lastUser = last.Content
				}
			}
			if n == 1 {
				return provider.Result{
					Content: "I cannot search the web from here.",
					Usage:   provider.Usage{PromptTokens: 20, CompletionTokens: 12},
				}
			}
			return provider.Result{
				Content: "ok, curling github.com/james-see",
				Usage:   provider.Usage{PromptTokens: 40, CompletionTokens: 10},
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	r, err := mgr.Execute(ctx, Options{Goal: "search the web for james-see github", Root: dir, Prov: mock, MaxSteps: 4})
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if r.State != StateCompleted {
		t.Fatalf("want completed got %s", r.State)
	}
	if !mgr.Hub.Get().AwaitReply {
		t.Fatal("expected await reply after complete")
	}
	r, err = mgr.Followup(ctx, "try curl instead")
	if err != nil {
		t.Fatalf("follow-up: %v", err)
	}
	if r.State != StateCompleted {
		t.Fatalf("follow-up want completed got %s", r.State)
	}
	if lastUser != "try curl instead" {
		t.Fatalf("session last user %q", lastUser)
	}
	if n < 2 {
		t.Fatalf("expected second generate, got %d", n)
	}
	evs, err := mgr.Store.ListEvents(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	var sawUser bool
	for _, ev := range evs {
		if ev.Type == event.UserMessage {
			sawUser = true
		}
	}
	if !sawUser {
		t.Fatalf("missing user.message: %s", types(evs))
	}
	for _, ev := range evs {
		if ev.Type == event.GoalShifted {
			t.Fatalf("clarification should not shift goal: %s", types(evs))
		}
	}
	if got := mgr.Hub.Get().ActiveGoal; got != "search the web for james-see github" {
		t.Fatalf("active goal %q", got)
	}
	if mgr.sess.opts.Goal != "search the web for james-see github" {
		t.Fatalf("phase goal %q", mgr.sess.opts.Goal)
	}
}

func TestFollowupResetsReflexPhase(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Judge.Model = ""
	cfg.Reflex.Detectors.TokenBurn.Threshold = 200
	cfg.Reflex.Detectors.Stagnation.Actions = 20
	mgr, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()

	n := 0
	mock := &provider.Mock{
		Name: "mock",
		Handler: func(provider.Request) provider.Result {
			n++
			if n == 1 {
				return provider.Result{
					Content: "first turn done",
					Usage:   provider.Usage{PromptTokens: 10, CompletionTokens: 5000},
				}
			}
			if n < 6 {
				return provider.Result{
					ToolCalls: []provider.ToolCall{{
						ID: fmt.Sprintf("%d", n), Name: "shell",
						Arguments: fmt.Sprintf(`{"command":"echo phase-%d"}`, n),
					}},
					Usage: provider.Usage{PromptTokens: 10, CompletionTokens: 10},
				}
			}
			return provider.Result{
				Content: "second turn done",
				Usage:   provider.Usage{PromptTokens: 10, CompletionTokens: 10},
			}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := mgr.Execute(ctx, Options{Goal: "original goal", Root: dir, Prov: mock, MaxSteps: 4}); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	r, err := mgr.Followup(ctx, "now do something else")
	if err != nil {
		t.Fatalf("follow-up: %v", err)
	}
	if r.State != StateCompleted {
		t.Fatalf("want completed got %s", r.State)
	}
	evs, err := mgr.Store.ListEvents(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Type == event.StagnationDetected || ev.Type == event.LoopDetected {
			t.Fatalf("phase reset should not inherit first-turn burn: %s", types(evs))
		}
	}
	if mgr.Hub.Get().TokensCompletion < 5000 {
		t.Fatal("session token totals should still accumulate")
	}
}

func TestFollowupShiftsOnNewObjective(t *testing.T) {
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

	n := 0
	mock := &provider.Mock{
		Name: "mock",
		Handler: func(provider.Request) provider.Result {
			n++
			return provider.Result{Content: "ok", Usage: provider.Usage{CompletionTokens: 4}}
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := mgr.Execute(ctx, Options{Goal: "fix the auth tests", Root: dir, Prov: mock, MaxSteps: 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.Followup(ctx, "scratch that, write a new billing report"); err != nil {
		t.Fatal(err)
	}
	if mgr.sess.opts.Goal != "scratch that, write a new billing report" {
		t.Fatalf("goal %q", mgr.sess.opts.Goal)
	}
	evs, err := mgr.Store.ListEvents(ctx, mgr.rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	var saw bool
	for _, ev := range evs {
		if ev.Type == event.GoalShifted {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("missing goal.shifted: %s", types(evs))
	}
}

func types(evs []event.Event) string {
	b, _ := json.Marshal(func() []string {
		var t []string
		for _, e := range evs {
			t = append(t, e.Type)
		}
		return t
	}())
	return string(b)
}

func initGit(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "t@t")
	run("git", "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "README")
	run("git", "commit", "-m", "init")
}
