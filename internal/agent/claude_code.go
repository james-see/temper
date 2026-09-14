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

// ClaudeCode supervises the Claude Code CLI/TUI as a Temper sidecar.
//
// Surfaces (documented by Anthropic):
//   - launch:  `claude` or `claude "<seed>"`; attach via `claude --resume <id>`
//   - poll:    ~/.claude/projects/<encoded-cwd>/<session-id>.jsonl
//   - inject:  TUI typing, else `claude -p --resume <id> --output-format json "<prompt>"`
type ClaudeCode struct {
	Command  string
	Spawn    bool
	LookPath LookPathFunc
	Exec     ExecFunc
	OpenTerm OpenTermFunc
	TypeTerm TypeTermFunc
	Now      func() time.Time
	// ConfigDir overrides CLAUDE_CONFIG_DIR / ~/.claude (tests inject).
	ConfigDir string

	term *term.Handle

	mu        sync.Mutex
	bin       string
	workspace string
	sessionID string
	logPath   string
	seenIDs   map[string]bool
	logCur    claudeCursor
	argv      []string
	started   time.Time
	queue     []Ingest
	snapOnce  sync.Once
	snapDone  chan struct{}
}

// NewClaudeCode constructs a Claude Code sidecar. command defaults to "claude".
func NewClaudeCode(command string, spawn bool) *ClaudeCode {
	if command == "" {
		command = "claude"
	}
	return &ClaudeCode{
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

func (c *ClaudeCode) ID() string { return "claude-code" }

func (c *ClaudeCode) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{ToolCalls: true, Resume: true}, nil
}

func (c *ClaudeCode) Start(_ context.Context, req TaskRequest) (Session, error) {
	look := c.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look(c.Command)
	if err != nil {
		return Session{}, fmt.Errorf("claude not on PATH: %w", err)
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
	c.argv = claudeLaunchArgs(req.Prompt, req.Session)
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

func (c *ClaudeCode) LaunchArgs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.argv...)
}

func (c *ClaudeCode) LaunchLine() string {
	return term.CommandLine(c.LaunchArgs(), c.workspace)
}

func claudeLaunchArgs(seed, session string) []string {
	seed = strings.TrimSpace(seed)
	session = strings.TrimSpace(session)
	if session != "" {
		return []string{"claude", "--resume", session}
	}
	if seed != "" {
		return []string{"claude", seed}
	}
	return []string{"claude"}
}

func (c *ClaudeCode) Resume(_ context.Context, id string) (Session, error) {
	if id != "" {
		c.Bind(id)
	}
	return Session{ID: c.SessionID()}, nil
}

func (c *ClaudeCode) Interrupt(context.Context, string) error {
	// Escape interrupts the current turn in the Claude Code TUI.
	esc := "\x1b"
	if c.TypeTerm != nil {
		return c.TypeTerm(esc)
	}
	if c.term != nil && c.term.CanType() {
		return c.term.Type(esc)
	}
	return nil
}

func (c *ClaudeCode) Events(context.Context, string) (<-chan Event, error) {
	return nil, fmt.Errorf("use Poll")
}

func (c *ClaudeCode) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

func (c *ClaudeCode) Workspace() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.workspace
}

func (c *ClaudeCode) Bind(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = id
	if id != "" {
		if p := findClaudeSessionLog(c.projectDirLocked(), id); p != "" {
			c.logPath = p
		}
	}
}

func (c *ClaudeCode) beginSnapshot() {
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

func (c *ClaudeCode) waitSnapshot(ctx context.Context) {
	if c.snapDone == nil {
		return
	}
	select {
	case <-ctx.Done():
	case <-c.snapDone:
	}
}

func (c *ClaudeCode) Discover(ctx context.Context) (string, bool) {
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
	id, ok := pickClaudeUnseen(rows, c.seenIDs)
	if !ok {
		return "", false
	}
	c.sessionID = id
	c.seenIDs[id] = true
	if p := findClaudeSessionLog(c.projectDirLocked(), id); p != "" {
		c.logPath = p
	}
	return id, true
}

func (c *ClaudeCode) Inject(ctx context.Context, prompt string) error {
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
			return fmt.Errorf("claude session not bound yet")
		}
	}
	_, err := c.run(ctx, c.bin, "-p", "--resume", id, "--output-format", "json", prompt)
	return err
}

func (c *ClaudeCode) Queue(ings ...Ingest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queue = append(c.queue, ings...)
}

func (c *ClaudeCode) Poll(ctx context.Context) ([]Ingest, error) {
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

func (c *ClaudeCode) snapshotIDs(context.Context) error {
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

func (c *ClaudeCode) listSessions() ([]string, error) {
	return listClaudeSessionIDs(c.projectDir())
}

func (c *ClaudeCode) pollLog(id string) ([]Ingest, error) {
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
	evs, next := parseClaudeJSONL(string(raw), cur)
	c.mu.Lock()
	c.logCur = next
	c.mu.Unlock()
	return evs, nil
}

func (c *ClaudeCode) sessionLogPath(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.logPath != "" && strings.Contains(c.logPath, id) {
		return c.logPath
	}
	p := findClaudeSessionLog(c.projectDirLocked(), id)
	c.logPath = p
	return p
}

func (c *ClaudeCode) projectDir() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.projectDirLocked()
}

func (c *ClaudeCode) projectDirLocked() string {
	root := c.configDirLocked()
	ws := c.workspace
	if ws == "" {
		ws, _ = os.Getwd()
	}
	return filepath.Join(root, "projects", encodeClaudeProjectDir(ws))
}

func (c *ClaudeCode) configDirLocked() string {
	if c.ConfigDir != "" {
		return c.ConfigDir
	}
	return claudeConfigDir()
}

func (c *ClaudeCode) run(ctx context.Context, name string, args ...string) (string, error) {
	fn := c.Exec
	if fn == nil {
		fn = defaultExec
	}
	if name == "" {
		name = c.Command
	}
	return fn(ctx, name, args)
}

func (c *ClaudeCode) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func claudeConfigDir() string {
	if d := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".claude"
	}
	return filepath.Join(home, ".claude")
}
