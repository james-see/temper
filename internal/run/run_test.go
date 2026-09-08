package run

import (
	"context"
	"encoding/json"
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
