package run

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/reflex"
	"github.com/james-see/temper/internal/store"
	"github.com/james-see/temper/internal/workspace"
)

// setupRecovery builds a manager with a hand-rolled session: a git repo whose
// committed state is "good" with uncommitted "bad" changes on top.
func setupRecovery(t *testing.T, action string) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	initGit(t, dir)
	ws, err := workspace.New(dir, filepath.Join(dir, ".temper"))
	if err != nil {
		t.Fatal(err)
	}
	ws.Attach()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("good"), 0o644); err != nil {
		t.Fatal(err)
	}
	good, err := ws.Checkpoint("good state")
	if err != nil || good == "" {
		t.Fatalf("%s %v", good, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Workspace.Root = dir
	mgr, err := Open(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.Close() })
	mgr.rec = store.Run{ID: "rc1", State: StateExecuting}
	mgr.sess = &session{
		ws:             ws,
		opts:           Options{Goal: "recover me"},
		lastCheckpoint: good,
		pending:        &pendingRec{Action: action, Assess: reflex.Assessment{State: reflex.Stalled}},
	}
	return mgr, dir
}

func sawEvent(t *testing.T, mgr *Manager, typ string) bool {
	t.Helper()
	evs, err := mgr.Store.ListEvents(context.Background(), mgr.rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func TestApplyPendingRollback(t *testing.T) {
	mgr, dir := setupRecovery(t, "rollback")
	ctx := context.Background()
	emit := func(typ, actor string, data any) { mgr.emit(ctx, typ, actor, data) }
	if err := mgr.applyPending(ctx, emit); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(b) != "good" {
		t.Fatalf("rollback must restore checkpoint: %q %v", b, err)
	}
	if !sawEvent(t, mgr, event.CheckpointRestored) {
		t.Fatal("missing checkpoint.restored")
	}
	if !sawEvent(t, mgr, event.RecoveryCompleted) {
		t.Fatal("missing recovery.completed")
	}
	if mgr.sess.pending != nil {
		t.Fatal("pending must clear")
	}
}

func TestApplyPendingFork(t *testing.T) {
	mgr, dir := setupRecovery(t, "fork")
	ctx := context.Background()
	emit := func(typ, actor string, data any) { mgr.emit(ctx, typ, actor, data) }
	if err := mgr.applyPending(ctx, emit); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(b) != "good" {
		t.Fatalf("fork must reset to checkpoint: %q %v", b, err)
	}
	cmd := exec.Command("git", "show", "temper/rc1/attempt-1:a.txt")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil || string(out) != "bad" {
		t.Fatalf("attempt branch must preserve bad state: %q %v", out, err)
	}
	if mgr.sess.forkAttempts != 1 {
		t.Fatalf("fork attempts %d", mgr.sess.forkAttempts)
	}
	if !sawEvent(t, mgr, event.RecoveryCompleted) {
		t.Fatal("missing recovery.completed")
	}
}

func TestApplyPendingRollbackWithoutCheckpointHeld(t *testing.T) {
	mgr, _ := setupRecovery(t, "rollback")
	mgr.sess.lastCheckpoint = ""
	ctx := context.Background()
	emit := func(typ, actor string, data any) { mgr.emit(ctx, typ, actor, data) }
	if err := mgr.applyPending(ctx, emit); err != nil {
		t.Fatal(err)
	}
	if mgr.sess.pending == nil || !mgr.sess.pending.Held {
		t.Fatal("missing checkpoint must hold for human")
	}
	if !sawEvent(t, mgr, event.RecoveryFailed) {
		t.Fatal("missing recovery.failed")
	}
}
