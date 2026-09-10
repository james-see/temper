package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	cursorconnect "github.com/james-see/cursor-connect"

	"github.com/james-see/temper/internal/event"
)

// Cursor attaches to a running Cursor.app composer via cursor-connect.
// Unofficial local transcripts + stop-hook inject. Not affiliated with Cursor.
type Cursor struct {
	Home string // override ~/.cursor for tests

	mu        sync.Mutex
	client    *cursorconnect.Client
	handle    cursorconnect.Handle
	workspace string
	session   string
	line      string
	queue     []Ingest
}

func NewCursor() *Cursor { return &Cursor{} }

func (c *Cursor) ID() string { return "cursor" }

func (c *Cursor) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{ToolCalls: true, Resume: true}, nil
}

func (c *Cursor) LaunchLine() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.line != "" {
		return c.line
	}
	return "cursor-connect ide-transcripts (unofficial)"
}

func (c *Cursor) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session
}

func (c *Cursor) Info() (workspace, preview string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.handle != nil {
		s := c.handle.Session()
		return s.Workspace.Path, s.Preview
	}
	return c.workspace, ""
}

func (c *Cursor) Events(context.Context, string) (<-chan Event, error) {
	return nil, fmt.Errorf("use Poll")
}

func (c *Cursor) Interrupt(context.Context, string) error { return nil }

func (c *Cursor) Resume(_ context.Context, sessionID string) (Session, error) {
	client, err := c.open()
	if err != nil {
		return Session{}, err
	}
	c.mu.Lock()
	root := c.workspace
	c.mu.Unlock()
	if root == "" {
		return Session{}, fmt.Errorf("cursor workspace required")
	}
	ws, err := client.ResolveWorkspace(root)
	if err != nil {
		return Session{}, err
	}
	h, err := client.Attach(ws, sessionID)
	if err != nil {
		return Session{}, err
	}
	c.mu.Lock()
	c.client = client
	c.handle = h
	c.session = h.Session().ID
	c.line = "watching " + h.Session().Path
	c.mu.Unlock()
	return Session{ID: h.Session().ID}, nil
}

func (c *Cursor) Start(_ context.Context, req TaskRequest) (Session, error) {
	client, err := c.open()
	if err != nil {
		return Session{}, err
	}
	root := req.Workspace
	if root == "" {
		return Session{}, fmt.Errorf("cursor workspace required")
	}
	ws, err := client.ResolveWorkspace(root)
	if err != nil {
		return Session{}, err
	}
	h, err := client.Attach(ws, req.Session)
	if err != nil {
		return Session{}, err
	}
	c.mu.Lock()
	c.client = client
	c.handle = h
	c.workspace = root
	c.session = h.Session().ID
	c.line = "watching " + h.Session().Path
	c.mu.Unlock()
	if p := strings.TrimSpace(req.Prompt); p != "" {
		if err := h.Inject(context.Background(), p); err != nil && !IsInjectUnavailable(err) {
			return Session{ID: h.Session().ID}, err
		}
	}
	return Session{ID: h.Session().ID}, nil
}

func (c *Cursor) open() (*cursorconnect.Client, error) {
	if c.Home != "" {
		return &cursorconnect.Client{Home: c.Home}, nil
	}
	return cursorconnect.Open()
}

func (c *Cursor) Inject(ctx context.Context, prompt string) error {
	c.mu.Lock()
	h := c.handle
	c.mu.Unlock()
	if h == nil {
		return fmt.Errorf("cursor session not attached")
	}
	return h.Inject(ctx, prompt)
}

func (c *Cursor) Queue(ings ...Ingest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue = append(c.queue, ings...)
}

func (c *Cursor) Poll(ctx context.Context) ([]Ingest, error) {
	c.mu.Lock()
	queued := c.queue
	c.queue = nil
	h := c.handle
	c.mu.Unlock()
	out := append([]Ingest(nil), queued...)
	if h == nil {
		return out, nil
	}
	evs, err := h.Poll(ctx)
	if err != nil {
		return out, err
	}
	sid := ""
	if h != nil {
		sid = h.Session().ID
	}
	for _, ev := range evs {
		ing, ok := ingestCursor(ev)
		if !ok {
			continue
		}
		if ing.Data == nil {
			ing.Data = map[string]any{}
		}
		ing.Data["agent"] = "cursor"
		if sid != "" {
			ing.Data["session"] = sid
		}
		out = append(out, ing)
	}
	return out, nil
}

func ingestCursor(ev cursorconnect.Event) (Ingest, bool) {
	switch ev.Kind {
	case cursorconnect.KindUser:
		return Ingest{Type: event.UserMessage, Data: map[string]any{"text": ev.Text}}, true
	case cursorconnect.KindAssistant:
		if strings.TrimSpace(ev.Text) == "" {
			return Ingest{}, false
		}
		return Ingest{Type: event.ModelCompleted, Data: map[string]any{"text": ev.Text}}, true
	case cursorconnect.KindToolUse:
		args := ""
		if len(ev.ToolInput) > 0 {
			args = string(ev.ToolInput)
		}
		return Ingest{Type: event.ToolRequested, Data: map[string]any{"tool": ev.ToolName, "args": args}}, true
	default:
		return Ingest{}, false
	}
}

// IsInjectUnavailable reports a cursor-connect turn-boundary inject failure.
func IsInjectUnavailable(err error) bool {
	return err != nil && errors.Is(err, cursorconnect.ErrInjectUnavailable)
}

