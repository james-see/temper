package agent

import (
	"context"
	"strings"
	"testing"
)

type stubSide struct {
	id      string
	session string
	injects []string
}

func (s *stubSide) ID() string { return s.id }
func (s *stubSide) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{}, nil
}
func (s *stubSide) Start(context.Context, TaskRequest) (Session, error) {
	return Session{ID: s.session}, nil
}
func (s *stubSide) Resume(context.Context, string) (Session, error) {
	return Session{ID: s.session}, nil
}
func (s *stubSide) Interrupt(context.Context, string) error { return nil }
func (s *stubSide) Events(context.Context, string) (<-chan Event, error) {
	return nil, nil
}
func (s *stubSide) Poll(context.Context) ([]Ingest, error) { return nil, nil }
func (s *stubSide) Inject(_ context.Context, prompt string) error {
	s.injects = append(s.injects, prompt)
	return nil
}
func (s *stubSide) SessionID() string  { return s.session }
func (s *stubSide) LaunchLine() string { return s.id }

func TestMuxInjectsAll(t *testing.T) {
	a := &stubSide{id: "cursor", session: "a"}
	b := &stubSide{id: "hermes", session: "b"}
	m := NewMux("control", []Sidecar{a, b})
	if !strings.Contains(m.SessionID(), "a") || !strings.Contains(m.SessionID(), "b") {
		t.Fatalf("ids %s", m.SessionID())
	}
	if _, err := m.Start(context.Background(), TaskRequest{Prompt: "fix it"}); err != nil {
		t.Fatal(err)
	}
	if len(a.injects) != 1 || a.injects[0] != "fix it" || len(b.injects) != 1 {
		t.Fatalf("injects %v %v", a.injects, b.injects)
	}
}
