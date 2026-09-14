package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/james-see/temper/internal/term"
)

// Codex supervises the OpenAI Codex CLI/TUI as a Temper sidecar.
//
// Surfaces (documented by OpenAI):
//   - launch:  `codex` or `codex "<seed>"`; attach via `codex resume <id>`
//   - poll:    ~/.codex/sessions/**/<session-id>.jsonl (CODEX_HOME override)
//   - inject:  TUI typing, else `codex exec resume <id> --json "<prompt>"`
type Codex struct {
	Command  string
	Spawn    bool
	LookPath LookPathFunc
	Exec     ExecFunc
	OpenTerm OpenTermFunc
	TypeTerm TypeTermFunc
	Now      func() time.Time
	// SessionsRoot overrides CODEX_HOME/sessions (tests inject).
	SessionsRoot string

	term *term.Handle

	mu        sync.Mutex
	bin       string
	workspace string
	sessionID string
	logPath   string
	seenIDs   map[string]bool
	logCur    codexCursor
	argv      []string
	started   time.Time
	queue     []Ingest
	snapOnce  sync.Once
	snapDone  chan struct{}
}

// NewCodex constructs a Codex sidecar. command defaults to "codex".
func NewCodex(command string, spawn bool) *Codex {
	if command == "" {
		command = "codex"
	}
	return &Codex{
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

func (c *Codex) ID() string { return "codex" }

func (c *Codex) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{ToolCalls: true, Resume: true, ModelOverride: true}, nil
}

func (c *Codex) Start(_ context.Context, req TaskRequest) (Session, error) {
	look := c.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look(c.Command)
	if err != nil {
		return Session{}, fmt.Errorf("codex not on PATH: %w", err)
	}
	c.bin = bin
	c.workspace = req.Workspace
	if c.workspace == "" {
		c.workspace, _ = os.Getwd()
	}
	if req.Session != "" {
		c.Bind(req.Session)
	}
	c.started = c.now()
	c.argv = codexLaunchArgs(req.Prompt, req.Session, req.Model)
	c.beginSnapshot()
	if c.Spawn {
		open := c.OpenTerm
		if open == nil {
			open = term.Open
		}
		handle, err := open(c.argv, c.workspace)
		if err != nil {
			return Session{}, err
		}
		c.term = handle
		if c.TypeTerm == nil && handle.CanType() {
			c.TypeTerm = handle.Type
		}
	}
	return Session{ID: req.RunID}, nil
}

func (c *Codex) LaunchArgs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.argv...)
}

func (c *Codex) LaunchLine() string {
	return term.CommandLine(c.LaunchArgs(), c.workspace)
}

func codexLaunchArgs(seed, session, model string) []string {
	seed = strings.TrimSpace(seed)
	session = strings.TrimSpace(session)
	model = strings.TrimSpace(model)
	if session != "" {
		args := []string{"codex", "resume", session}
		if model != "" {
			args = append(args, "--model", model)
		}
		return args
	}
	args := []string{"codex"}
	if model != "" {
		args = append(args, "--model", model)
	}
	if seed != "" {
		args = append(args, seed)
	}
	return args
}

func (c *Codex) Resume(_ context.Context, id string) (Session, error) {
	if id != "" {
		c.Bind(id)
	}
	return Session{ID: c.SessionID()}, nil
}

func (c *Codex) Interrupt(context.Context, string) error {
	esc := "\x1b"
	if c.TypeTerm != nil {
		return c.TypeTerm(esc)
	}
	if c.term != nil && c.term.CanType() {
		return c.term.Type(esc)
	}
	return nil
}

func (c *Codex) Events(context.Context, string) (<-chan Event, error) {
	return nil, fmt.Errorf("use Poll")
}

func (c *Codex) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

func (c *Codex) Workspace() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.workspace
}

func (c *Codex) Bind(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = id
	if id != "" {
		if p := findCodexSessionLog(c.sessionsRootLocked(), id); p != "" {
			c.logPath = p
		}
	}
}

func (c *Codex) beginSnapshot() {
	if c.snapDone == nil {
		c.snapDone = make(chan struct{})
	}
	go func() {
		defer c.snapOnce.Do(func() { close(c.snapDone) })
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_ = c.snapshotIDs(ctx)
	}()
}

func (c *Codex) waitSnapshot(ctx context.Context) {
	if c.snapDone == nil {
		return
	}
	select {
	case <-ctx.Done():
	case <-c.snapDone:
	}
}

func (c *Codex) Discover(ctx context.Context) (string, bool) {
	if id := c.SessionID(); id != "" {
		return id, true
	}
	c.waitSnapshot(ctx)
	rows, err := c.listSessions()
	if err != nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	id, ok := pickCodexUnseen(rows, c.seenIDs)
	if !ok {
		return "", false
	}
	c.sessionID = id
	c.seenIDs[id] = true
	if p := findCodexSessionLog(c.sessionsRootLocked(), id); p != "" {
		c.logPath = p
	}
	return id, true
}

func (c *Codex) Inject(ctx context.Context, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("empty recovery prompt")
	}
	if c.TypeTerm != nil {
		return c.TypeTerm(prompt)
	}
	if c.term != nil && c.term.CanType() {
		return c.term.Type(prompt)
	}
	id := c.SessionID()
	if id == "" {
		var ok bool
		id, ok = c.Discover(ctx)
		if !ok {
			return fmt.Errorf("codex session not bound yet")
		}
	}
	_, err := c.run(ctx, c.bin, "exec", "resume", id, "--json", prompt)
	return err
}

func (c *Codex) Queue(ings ...Ingest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue = append(c.queue, ings...)
}

func (c *Codex) Poll(ctx context.Context) ([]Ingest, error) {
	if c.SessionID() == "" {
		c.Discover(ctx)
	}
	id := c.SessionID()
	c.mu.Lock()
	queued := c.queue
	c.queue = nil
	c.mu.Unlock()
	if id == "" && len(queued) == 0 {
		return queued, nil
	}
	out := append([]Ingest(nil), queued...)
	if id == "" {
		return out, nil
	}
	msgs, err := c.pollLog(id)
	if err != nil {
		return out, err
	}
	return append(out, msgs...), nil
}

func (c *Codex) snapshotIDs(context.Context) error {
	ids, err := c.listSessions()
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seenIDs == nil {
		c.seenIDs = map[string]bool{}
	}
	for _, id := range ids {
		c.seenIDs[id] = true
	}
	return nil
}

func (c *Codex) listSessions() ([]string, error) {
	return listCodexSessionIDs(c.sessionsRoot())
}

func (c *Codex) pollLog(id string) ([]Ingest, error) {
	path := c.sessionLogPath(id)
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	c.mu.Lock()
	cur := c.logCur
	c.mu.Unlock()
	evs, next := parseCodexJSONL(string(raw), cur)
	c.mu.Lock()
	c.logCur = next
	c.mu.Unlock()
	return evs, nil
}

func (c *Codex) sessionLogPath(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.logPath != "" && strings.Contains(c.logPath, id) {
		return c.logPath
	}
	p := findCodexSessionLog(c.sessionsRootLocked(), id)
	c.logPath = p
	return p
}

func (c *Codex) sessionsRoot() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionsRootLocked()
}

func (c *Codex) sessionsRootLocked() string {
	if c.SessionsRoot != "" {
		return c.SessionsRoot
	}
	return codexSessionsRoot()
}

func (c *Codex) run(ctx context.Context, name string, args ...string) (string, error) {
	fn := c.Exec
	if fn == nil {
		fn = defaultExec
	}
	if name == "" {
		name = c.Command
	}
	return fn(ctx, name, args)
}

func (c *Codex) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func codexHome() string {
	if d := strings.TrimSpace(os.Getenv("CODEX_HOME")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".codex"
	}
	return filepath.Join(home, ".codex")
}

func codexSessionsRoot() string {
	return filepath.Join(codexHome(), "sessions")
}
