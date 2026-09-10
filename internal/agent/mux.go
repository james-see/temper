package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Mux polls and steers several sidecars as one control-plane attach.
type Mux struct {
	name  string
	items []Sidecar

	mu   sync.Mutex
	line string
}

func NewMux(name string, items []Sidecar) *Mux {
	return &Mux{name: name, items: items, line: fmt.Sprintf("%s × %d sessions", name, len(items))}
}

func (m *Mux) ID() string {
	if m.name != "" {
		return m.name
	}
	return "mux"
}

func (m *Mux) LaunchLine() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.line
}

func (m *Mux) SessionID() string {
	var ids []string
	for _, it := range m.items {
		if id := it.SessionID(); id != "" {
			ids = append(ids, id)
		}
	}
	return strings.Join(ids, ",")
}

func (m *Mux) Items() []Sidecar { return m.items }

func (m *Mux) Capabilities(ctx context.Context) (Capabilities, error) {
	var cap Capabilities
	for _, it := range m.items {
		c, err := it.Capabilities(ctx)
		if err != nil {
			continue
		}
		cap.ToolCalls = cap.ToolCalls || c.ToolCalls
		cap.Resume = cap.Resume || c.Resume
		cap.Streaming = cap.Streaming || c.Streaming
	}
	return cap, nil
}

func (m *Mux) Start(ctx context.Context, req TaskRequest) (Session, error) {
	if p := strings.TrimSpace(req.Prompt); p != "" {
		if err := m.Inject(ctx, p); err != nil {
			return Session{ID: m.SessionID()}, err
		}
	}
	return Session{ID: m.SessionID()}, nil
}

func (m *Mux) Resume(ctx context.Context, id string) (Session, error) {
	return Session{ID: id}, nil
}

func (m *Mux) Interrupt(ctx context.Context, id string) error {
	var first error
	for _, it := range m.items {
		if err := it.Interrupt(ctx, it.SessionID()); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (m *Mux) Events(context.Context, string) (<-chan Event, error) {
	return nil, fmt.Errorf("use Poll")
}

func (m *Mux) Inject(ctx context.Context, prompt string) error {
	var first error
	for _, it := range m.items {
		if err := it.Inject(ctx, prompt); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (m *Mux) Poll(ctx context.Context) ([]Ingest, error) {
	var out []Ingest
	var first error
	for _, it := range m.items {
		ings, err := it.Poll(ctx)
		if err != nil && first == nil {
			first = err
		}
		for _, ing := range ings {
			if ing.Data == nil {
				ing.Data = map[string]any{}
			}
			ing.Data["agent"] = it.ID()
			ing.Data["session"] = it.SessionID()
			out = append(out, ing)
		}
	}
	return out, first
}
