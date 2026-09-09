package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/james-see/temper/internal/term"
)

var sessionIDRe = regexp.MustCompile(`(\d{8}_\d{6}_[a-f0-9]+)\s*$`)

type ExecFunc func(ctx context.Context, name string, args []string) (string, error)
type OpenTermFunc func(argv []string, dir string) (*term.Handle, error)
type TypeTermFunc func(text string) error
type LookPathFunc func(file string) (string, error)

type Ingest struct {
	Type string
	Data map[string]any
}

type Hermes struct {
	Command  string
	Spawn    bool
	LookPath LookPathFunc
	Exec     ExecFunc
	OpenTerm OpenTermFunc
	TypeTerm TypeTermFunc
	Now      func() time.Time

	term *term.Handle

	mu         sync.Mutex
	bin        string
	workspace  string
	sessionID  string
	seenIDs    map[string]bool
	exportSeen int
	logSeen    map[string]bool
	argv       []string
	started    time.Time
	queue      []Ingest
	snapOnce   sync.Once
	snapDone   chan struct{}
}

func NewHermes(command string, spawn bool) *Hermes {
	if command == "" {
		command = "hermes"
	}
	return &Hermes{
		Command:  command,
		Spawn:    spawn,
		LookPath: exec.LookPath,
		Exec:     defaultExec,
		OpenTerm: term.Open,
		Now:      time.Now,
		seenIDs:  map[string]bool{},
		logSeen:  map[string]bool{},
		snapDone: make(chan struct{}),
	}
}

func (h *Hermes) ID() string { return "hermes" }

func (h *Hermes) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{ToolCalls: true}, nil
}

func (h *Hermes) Start(_ context.Context, req TaskRequest) (Session, error) {
	look := h.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look(h.Command)
	if err != nil {
		return Session{}, fmt.Errorf("hermes not on PATH: %w", err)
	}
	h.bin = bin
	h.workspace = req.Workspace
	if h.workspace == "" {
		h.workspace, _ = os.Getwd()
	}
	// Hermes is a slow Python CLI (~10s for --version). Do not block launch on it.
	h.started = h.now()
	h.argv = hermesLaunchArgs(h.workspace, req.Prompt)
	h.beginSnapshot()
	if h.Spawn {
		open := h.OpenTerm
		if open == nil {
			open = term.Open
		}
		handle, err := open(h.argv, h.workspace)
		if err != nil {
			return Session{}, err
		}
		h.term = handle
		if h.TypeTerm == nil && handle.CanType() {
			h.TypeTerm = handle.Type
		}
	}
	return Session{ID: req.RunID}, nil
}

func (h *Hermes) LaunchArgs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.argv...)
}

func (h *Hermes) LaunchLine() string {
	return term.CommandLine(h.LaunchArgs(), h.workspace)
}

func hermesLaunchArgs(workspace, seed string) []string {
	// --source is a `hermes chat` flag only. On `hermes --tui` it is parsed as
	// the positional command, so `--source temper` becomes `invalid choice: temper`.
	if strings.TrimSpace(seed) != "" {
		return []string{"hermes", "chat", "--tui", "--in", workspace, "-q", seed}
	}
	return []string{"hermes", "--tui", "--in", workspace}
}

func (h *Hermes) Resume(context.Context, string) (Session, error) {
	return Session{ID: h.SessionID()}, nil
}

func (h *Hermes) Interrupt(context.Context, string) error { return nil }

func (h *Hermes) Events(context.Context, string) (<-chan Event, error) {
	return nil, fmt.Errorf("use Poll")
}

func (h *Hermes) SessionID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessionID
}

func (h *Hermes) Bind(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessionID = id
}

func (h *Hermes) beginSnapshot() {
	if h.snapDone == nil {
		h.snapDone = make(chan struct{})
	}
	go func() {
		defer h.snapOnce.Do(func() { close(h.snapDone) })
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		if err := h.snapshotIDs(ctx); err != nil {
			h.mu.Lock()
			if h.seenIDs == nil {
				h.seenIDs = map[string]bool{}
			}
			h.mu.Unlock()
		}
	}()
}

func (h *Hermes) waitSnapshot(ctx context.Context) {
	if h.snapDone == nil {
		return
	}
	select {
	case <-ctx.Done():
	case <-h.snapDone:
	}
}

func (h *Hermes) Discover(ctx context.Context) (string, bool) {
	if id := h.SessionID(); id != "" {
		return id, true
	}
	h.waitSnapshot(ctx)
	rows, err := h.listSessions(ctx)
	if err != nil {
		return "", false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	id, ok := pickUnseen(rows, h.seenIDs)
	if !ok {
		return "", false
	}
	h.sessionID = id
	h.seenIDs[id] = true
	return id, true
}

func (h *Hermes) Inject(ctx context.Context, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("empty recovery prompt")
	}
	// Prefer typing into the spawned TUI. hermes chat --resume is refused while
	// that TUI holds the session lease ("already has a live owner").
	if h.TypeTerm != nil {
		return h.TypeTerm(prompt)
	}
	if h.term.CanType() {
		return h.term.Type(prompt)
	}
	id := h.SessionID()
	if id == "" {
		var ok bool
		id, ok = h.Discover(ctx)
		if !ok {
			return fmt.Errorf("hermes session not bound yet")
		}
	}
	_, err := h.run(ctx, h.bin, "chat", "--oneshot", "-Q", "--resume", id, "-q", prompt)
	return err
}

func IsLiveOwner(err error) bool {
	return err != nil && strings.Contains(err.Error(), "already has a live owner")
}

func (h *Hermes) Queue(ings ...Ingest) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.queue = append(h.queue, ings...)
}

func (h *Hermes) Poll(ctx context.Context) ([]Ingest, error) {
	if h.SessionID() == "" {
		h.Discover(ctx)
	}
	id := h.SessionID()
	h.mu.Lock()
	queued := h.queue
	h.queue = nil
	h.mu.Unlock()
	if id == "" && len(queued) == 0 {
		return queued, nil
	}
	var out []Ingest
	out = append(out, queued...)
	if id == "" {
		return out, nil
	}
	var first error
	msgs, err := h.pollExport(ctx, id)
	if err != nil && first == nil {
		first = err
	} else {
		out = append(out, msgs...)
	}
	logs, err := h.pollLogs(ctx, id)
	if err != nil && first == nil {
		first = err
	} else {
		out = append(out, logs...)
	}
	return out, first
}

func (h *Hermes) snapshotIDs(ctx context.Context) error {
	ids, err := h.listSessions(ctx)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.seenIDs == nil {
		h.seenIDs = map[string]bool{}
	}
	for _, id := range ids {
		h.seenIDs[id] = true
	}
	return nil
}

func (h *Hermes) listSessions(ctx context.Context) ([]string, error) {
	args := []string{"sessions", "list", "--limit", "20"}
	if h.workspace != "" {
		args = append(args, "--workspace", h.workspace)
	}
	out, err := h.run(ctx, h.bin, args...)
	if err != nil {
		return nil, err
	}
	return parseSessionIDs(out), nil
}

func (h *Hermes) pollExport(ctx context.Context, id string) ([]Ingest, error) {
	out, err := h.run(ctx, h.bin, "sessions", "export", "--session-id", id, "--format", "jsonl", "-")
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	seen := h.exportSeen
	h.mu.Unlock()
	evs, next := parseExportOutput(out, seen)
	h.mu.Lock()
	h.exportSeen = next
	h.mu.Unlock()
	return evs, nil
}

func (h *Hermes) pollLogs(ctx context.Context, id string) ([]Ingest, error) {
	out, err := h.run(ctx, h.bin, "logs", "-n", "40", "--session", id)
	if err != nil {
		return nil, err
	}
	var evs []Ingest
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.logSeen == nil {
		h.logSeen = map[string]bool{}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || h.logSeen[line] {
			continue
		}
		if len(h.logSeen) > 1000 {
			h.logSeen = map[string]bool{}
		}
		h.logSeen[line] = true
		if ing, ok := classifyLogLine(line); ok {
			evs = append(evs, ing)
		}
	}
	return evs, nil
}

func (h *Hermes) run(ctx context.Context, name string, args ...string) (string, error) {
	fn := h.Exec
	if fn == nil {
		fn = defaultExec
	}
	if name == "" {
		name = h.Command
	}
	return fn(ctx, name, args)
}

func (h *Hermes) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func defaultExec(ctx context.Context, name string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg != "" {
			return stdout.String(), fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

func parseSessionIDs(out string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		m := sessionIDRe.FindStringSubmatch(line)
		if len(m) < 2 || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		ids = append(ids, m[1])
	}
	return ids
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := strings.TrimSpace(asString(m[k])); s != "" {
			return s
		}
	}
	return ""
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
