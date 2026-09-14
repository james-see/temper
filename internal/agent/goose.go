package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/james-see/temper/internal/term"
)

// Goose supervises the Goose CLI (block/goose) as a Temper sidecar.
//
// Goose is driven through its CLI surface:
//   - launch:   `goose run --text <goal> --interactive` seeds a session, then
//     stays in the TUI so Temper can type recovery prompts; a bare
//     `goose session` opens the interactive TUI with no seed.
//   - poll:     `goose session list --format json` discovers sessions and
//     `goose session export --session-id <id> --format json` reads the
//     transcript (Goose >= sqlite-backed versions; legacy .jsonl sessions
//     export through the same command).
//   - inject:   typing into the spawned TUI when available, else a one-shot
//     `goose run --resume --session-id <id> --text <prompt> --quiet`.
type Goose struct {
	Command  string
	Spawn    bool
	LookPath LookPathFunc
	Exec     ExecFunc
	OpenTerm OpenTermFunc
	TypeTerm TypeTermFunc
	Now      func() time.Time

	term *term.Handle

	mu        sync.Mutex
	bin       string
	workspace string
	sessionID string
	seenIDs   map[string]bool
	argv      []string
	started   time.Time
	queue     []Ingest
	exportCur gooseCursor
	snapOnce  sync.Once
	snapDone  chan struct{}
	snapBegun bool
}

// NewGoose constructs a Goose sidecar. command defaults to "goose".
func NewGoose(command string, spawn bool) *Goose {
	if command == "" {
		command = "goose"
	}
	return &Goose{
		Command:  command,
		Spawn:    spawn,
		LookPath: exec.LookPath,
		Exec:     defaultExec,
		OpenTerm: term.Open,
		Now:      time.Now,
		seenIDs:  map[string]bool{},
		snapDone: make(chan struct{}),
	}
}

func (g *Goose) ID() string { return "goose" }

func (g *Goose) Capabilities(context.Context) (Capabilities, error) {
	// Honest: Temper observes tool calls through the session export, resumes
	// sessions via `goose run --resume`, and overrides models via --model.
	// Streaming, MCP, ACP, and subagents exist in Goose but are not driven
	// through this adapter yet.
	return Capabilities{ToolCalls: true, Resume: true, ModelOverride: true}, nil
}

func (g *Goose) Start(_ context.Context, req TaskRequest) (Session, error) {
	look := g.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look(g.Command)
	if err != nil {
		return Session{}, fmt.Errorf("goose not on PATH: %w", err)
	}
	g.mu.Lock()
	g.bin = bin
	g.mu.Unlock()
	g.workspace = req.Workspace
	if g.workspace == "" {
		g.workspace, _ = os.Getwd()
	}
	if req.Session != "" {
		g.Bind(req.Session)
	}
	g.started = g.now()
	g.argv = gooseLaunchArgs(req.Prompt, req.Model)
	g.beginSnapshot()
	if g.Spawn {
		open := g.OpenTerm
		if open == nil {
			open = term.Open
		}
		handle, err := open(g.argv, g.workspace)
		if err != nil {
			return Session{}, err
		}
		g.term = handle
		if g.TypeTerm == nil && handle.CanType() {
			g.TypeTerm = handle.Type
		}
	}
	return Session{ID: req.RunID}, nil
}

func (g *Goose) LaunchArgs() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.argv...)
}

func (g *Goose) LaunchLine() string {
	return term.CommandLine(g.LaunchArgs(), g.workspace)
}

// gooseLaunchArgs builds the Goose launch command. `goose run --text <seed>
// --interactive` runs the seed then stays in the TUI; with no seed a bare
// `goose session` opens the interactive TUI.
func gooseLaunchArgs(seed, model string) []string {
	args := []string{"goose"}
	if strings.TrimSpace(seed) != "" {
		args = append(args, "run", "--interactive", "--text", seed)
	} else {
		args = append(args, "session")
	}
	if m := strings.TrimSpace(model); m != "" {
		args = append(args, "--model", m)
	}
	return args
}

func (g *Goose) Resume(_ context.Context, id string) (Session, error) {
	if id != "" {
		g.Bind(id)
	}
	return Session{ID: g.SessionID()}, nil
}

func (g *Goose) Interrupt(context.Context, string) error { return nil }

func (g *Goose) Events(context.Context, string) (<-chan Event, error) {
	return nil, fmt.Errorf("use Poll")
}

func (g *Goose) SessionID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sessionID
}

func (g *Goose) Workspace() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.workspace
}

func (g *Goose) Bind(id string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sessionID = id
}

func (g *Goose) beginSnapshot() {
	if g.snapDone == nil {
		g.snapDone = make(chan struct{})
	}
	g.mu.Lock()
	g.snapBegun = true
	g.mu.Unlock()
	go func() {
		defer g.snapOnce.Do(func() { close(g.snapDone) })
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_ = g.snapshotIDs(ctx)
	}()
}

func (g *Goose) waitSnapshot(ctx context.Context) {
	g.mu.Lock()
	begun := g.snapBegun
	g.mu.Unlock()
	if !begun {
		return
	}
	select {
	case <-ctx.Done():
	case <-g.snapDone:
	}
}

// Discover binds the newest unseen Goose session. Sessions created before the
// sidecar started are ignored so pre-existing history is not attached.
func (g *Goose) Discover(ctx context.Context) (string, bool) {
	if id := g.SessionID(); id != "" {
		return id, true
	}
	g.waitSnapshot(ctx)
	ids, err := g.listSessions(ctx)
	if err != nil {
		return "", false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	id, ok := pickGooseUnseen(ids, g.seenIDs)
	if !ok {
		return "", false
	}
	g.sessionID = id
	g.seenIDs[id] = true
	return id, true
}

func (g *Goose) Inject(ctx context.Context, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("empty recovery prompt")
	}
	// Prefer typing into the spawned TUI so the live session stays the owner.
	if g.TypeTerm != nil {
		return g.TypeTerm(prompt)
	}
	if g.term != nil && g.term.CanType() {
		return g.term.Type(prompt)
	}
	id := g.SessionID()
	if id == "" {
		var ok bool
		id, ok = g.Discover(ctx)
		if !ok {
			return fmt.Errorf("goose session not bound yet")
		}
	}
	_, err := g.run(ctx, g.bin, "run", "--resume", "--session-id", id, "--text", prompt, "--quiet")
	return err
}

func (g *Goose) Queue(ings ...Ingest) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.queue = append(g.queue, ings...)
}

func (g *Goose) Poll(ctx context.Context) ([]Ingest, error) {
	if g.SessionID() == "" {
		g.Discover(ctx)
	}
	id := g.SessionID()
	g.mu.Lock()
	queued := g.queue
	g.queue = nil
	g.mu.Unlock()
	if id == "" && len(queued) == 0 {
		return queued, nil
	}
	out := append([]Ingest(nil), queued...)
	if id == "" {
		return out, nil
	}
	out2, err := g.pollExport(ctx, id)
	if err != nil {
		return out, err
	}
	return append(out, out2...), nil
}

func (g *Goose) snapshotIDs(ctx context.Context) error {
	ids, err := g.listSessions(ctx)
	if err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seenIDs == nil {
		g.seenIDs = map[string]bool{}
	}
	for _, id := range ids {
		g.seenIDs[id] = true
	}
	return nil
}

func (g *Goose) listSessions(ctx context.Context) ([]string, error) {
	out, err := g.run(ctx, g.bin, "session", "list", "--format", "json", "--limit", "20")
	if err != nil {
		return nil, err
	}
	return parseGooseSessionList(out, g.workspace), nil
}

func (g *Goose) pollExport(ctx context.Context, id string) ([]Ingest, error) {
	out, err := g.run(ctx, g.bin, "session", "export", "--session-id", id, "--format", "json")
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	cur := g.exportCur
	g.mu.Unlock()
	evs, next := parseGooseExport(out, cur)
	g.mu.Lock()
	g.exportCur = next
	g.mu.Unlock()
	return evs, nil
}

func (g *Goose) run(ctx context.Context, name string, args ...string) (string, error) {
	fn := g.Exec
	if fn == nil {
		fn = defaultExec
	}
	if name == "" {
		name = g.Command
	}
	return fn(ctx, name, args)
}

func (g *Goose) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}
