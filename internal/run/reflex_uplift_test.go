package run

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/provider"
)

// loopThenFixProvider repeats one failing action until a recovery injection
// appears in the request, then fixes the flag file. It models an agent
// stuck in a loop that only a recovery nudge can break. The marker is the
// recovery prompt's re-anchor line: the bare word "Reflex" is unsafe here
// because the workspace path (leaked into the system prompt) contains the
// test name.
func loopThenFixProvider() *provider.Mock {
	return &provider.Mock{
		Name: "mock",
		Handler: func(req provider.Request) provider.Result {
			fixed := false
			for _, m := range req.Messages {
				if strings.Contains(m.Content, "Re-anchor on goal:") {
					fixed = true
					break
				}
			}
			cmd := "echo loop > flag.txt"
			if fixed {
				cmd = "echo fixed > flag.txt"
			}
			return provider.Result{
				ToolCalls: []provider.ToolCall{{ID: "1", Name: "shell", Arguments: `{"command":"` + cmd + `"}`}},
				Usage:     provider.Usage{PromptTokens: 10, CompletionTokens: 5},
			}
		},
	}
}

// reflexUpliftOnce runs one loop-then-fix scenario, returning whether the run
// completed. Reflex on means the ladder may intervene; off means detection
// without intervention.
func reflexUpliftOnce(tb testing.TB, reflexOn bool) bool {
	tb.Helper()
	dir := tb.TempDir()
	initGit(tb, dir)
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	if reflexOn {
		cfg.Reflex.Mode = config.ReflexAuto
	} else {
		cfg.Reflex.Mode = config.ReflexOff
	}
	cfg.Reflex.Judge.Model = ""
	cfg.Evaluator.Test = `test "$(cat flag.txt)" = fixed`
	mgr, err := Open(cfg, dir)
	if err != nil {
		tb.Fatal(err)
	}
	defer mgr.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err = mgr.Execute(ctx, Options{Goal: "fix the flag", Root: dir, Prov: loopThenFixProvider(), MaxSteps: 8})
	return err == nil
}

func reflexUplift(tb testing.TB, iters int) (on, off int) {
	tb.Helper()
	for i := 0; i < iters; i++ {
		if reflexUpliftOnce(tb, true) {
			on++
		}
		if reflexUpliftOnce(tb, false) {
			off++
		}
	}
	return on, off
}

// TestReflexUplift is the maintained check that recovery intervention beats
// detection-only on a loop scenario. The benchmark below reports the rates.
func TestReflexUplift(t *testing.T) {
	on, off := reflexUplift(t, 2)
	t.Logf("reflex on %d/2 completed, off %d/2 completed", on, off)
	if on <= off {
		t.Fatalf("expected uplift with reflex on (on=%d off=%d)", on, off)
	}
}

// BenchmarkReflexCompletion measures completion rate with reflex enabled vs
// disabled over fixed loop-then-fix scenarios. It reports rates, not ns/op.
func BenchmarkReflexCompletion(b *testing.B) {
	const iters = 6
	on, off := reflexUplift(b, iters)
	b.ReportMetric(float64(on)/iters, "on_completed/op")
	b.ReportMetric(float64(off)/iters, "off_completed/op")
	b.Logf("reflex on %d/%d completed, off %d/%d completed", on, iters, off, iters)
}
