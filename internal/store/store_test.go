package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/james-see/temper/internal/event"
)

func TestAppendAndList(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "temper.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	r := Run{ID: "r1", Goal: "fix tests", State: "created", CreatedAt: time.Now().UTC()}
	if err := s.CreateRun(ctx, r); err != nil {
		t.Fatal(err)
	}
	seq, err := s.NextSequence(ctx, "r1")
	if err != nil || seq != 1 {
		t.Fatalf("seq %d %v", seq, err)
	}
	if err := s.AppendEvent(ctx, event.Event{
		ID: "e1", RunID: "r1", Sequence: 1, Type: event.RunStarted, Actor: "runtime", Data: Encode(map[string]string{"a": "b"}),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(ctx, "r1")
	if err != nil || got.Goal != "fix tests" {
		t.Fatalf("%+v %v", got, err)
	}
	evs, err := s.ListEvents(ctx, "r1")
	if err != nil || len(evs) != 1 || evs[0].Type != event.RunStarted {
		t.Fatalf("%+v %v", evs, err)
	}
}
