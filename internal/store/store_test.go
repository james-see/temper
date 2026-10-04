package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/james-see/temper/internal/event"
)

func TestArchiveRestoreDelete(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "temper.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateRun(ctx, Run{ID: "r1", Goal: "g", State: "done", Agent: "native"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(ctx, event.Event{ID: "r1-1", RunID: "r1", Sequence: 1, Type: event.RunCreated, Actor: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetArchived(ctx, "r1", true); err != nil {
		t.Fatal(err)
	}
	r, err := s.GetRun(ctx, "r1")
	if err != nil || !r.Archived {
		t.Fatalf("%+v %v", r, err)
	}
	if err := s.SetArchived(ctx, "r1", false); err != nil {
		t.Fatal(err)
	}
	if r, err := s.GetRun(ctx, "r1"); err != nil || r.Archived {
		t.Fatalf("restore %+v %v", r, err)
	}
	if err := s.DeleteRun(ctx, "r1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRun(ctx, "r1"); err == nil {
		t.Fatal("deleted run must be gone")
	}
	if evs, err := s.ListEvents(ctx, "r1"); err != nil || len(evs) != 0 {
		t.Fatalf("deleted events %+v %v", evs, err)
	}
}

func TestOpenMigratesExistingDB(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "temper.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRun(context.Background(), Run{ID: "r1", Goal: "g", State: "done"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Reopen exercises the duplicate-column-tolerant ALTER path.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if _, err := s2.GetRun(context.Background(), "r1"); err != nil {
		t.Fatal(err)
	}
}
