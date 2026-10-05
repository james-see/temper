package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/provider"
	"github.com/james-see/temper/internal/run"
)

// TestStatusOverlayShowsRoutingRationale proves the arbiter's routing
// rationale flows from Execute through the hub into the TUI status overlay.
func TestStatusOverlayShowsRoutingRationale(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	cfg.Reflex.Mode = config.ReflexOff
	cfg.Reflex.Judge.Model = ""
	mgr, err := run.Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = mgr.Execute(ctx, run.Options{
		Goal: "route me", Root: dir,
		Prov:     &provider.Mock{Name: "mock"},
		MaxSteps: 1,
	})
	m := New(Options{Hub: mgr.Hub})
	out := m.statusOverlay()
	if !strings.Contains(out, "routing") {
		t.Fatalf("missing routing line:\n%s", out)
	}
	if !strings.Contains(out, "probe usable providers") {
		t.Fatalf("missing rationale:\n%s", out)
	}
}
