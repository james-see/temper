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

// Muse supervises the Muse Code CLI/TUI as a Temper sidecar.
type Muse struct {
	Command  string
	Spawn    bool
	LookPath LookPathFunc
	Exec     ExecFunc
	OpenTerm OpenTermFunc
	TypeTerm TypeTermFunc
	Now      func() time.Time
	// SessionsRoot overrides the XDG Muse sessions directory (tests inject).
	SessionsRoot string

	term *term.Handle

	mu        sync.Mutex
	bin       string
	workspace string
	sessionID string
	logPath   string
	seenIDs   map[string]bool
	logCur    museCursor
	argv      []string
	started   time.Time
	queue     []Ingest
	snapOnce  sync.Once
	snapDone  chan struct{}
}

// NewMuse constructs a Muse sidecar. command defaults to "muse".
func NewMuse(command string, spawn bool) *Muse {
	if command == "" {
		command = "muse"
	}
	return &Muse{
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

func (m *Muse) ID() string { return "muse" }

func (m *Muse) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{ToolCalls: true, Resume: true}, nil
}

func (m *Muse) Start(_ context.Context, req TaskRequest) (Session, error) {
	look := m.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look(m.Command)
	if err != nil {
		return Session{}, fmt.Errorf("muse not on PATH: %w", err)
	}
	m.bin = bin
	m.workspace = req.Workspace
	if m.workspace == "" {
		m.workspace, _ = os.Getwd()
	}
	if req.Session != "" {
		m.Bind(req.Session)
	}
	m.started = m.now()
	m.argv = museLaunchArgs(req.Prompt, req.Session)
	if m.Spawn {
		// Snapshot before spawn so the new session cannot be marked seen.
		_ = m.snapshotIDs(context.Background())
		if m.snapDone == nil {
			m.snapDone = make(chan struct{})
		}
		m.snapOnce.Do(func() { close(m.snapDone) })
		open := m.OpenTerm
		if open == nil {
			open = term.Open
		}
		handle, err := open(m.argv, m.workspace)
		if err != nil {
			return Session{}, err
		}
		m.term = handle
		if m.TypeTerm == nil && handle.CanType() {
			m.TypeTerm = handle.Type
		}
	} else {
		m.beginSnapshot()
	}
	return Session{ID: req.RunID}, nil
}

func (m *Muse) LaunchArgs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.argv...)
}

func (m *Muse) LaunchLine() string {
	return term.CommandLine(m.LaunchArgs(), m.workspace)
}

func museLaunchArgs(seed, session string) []string {
	seed = strings.TrimSpace(seed)
	session = strings.TrimSpace(session)
	if session != "" {
		args := []string{"muse", "resume", session}
		if seed != "" {
			// Resume opens the TUI; seed is typed after attach when Temper injects.
			_ = seed
		}
		return args
	}
	if seed != "" {
		return []string{"muse", seed}
	}
	return []string{"muse"}
}

func (m *Muse) Resume(_ context.Context, id string) (Session, error) {
	if id != "" {
		m.Bind(id)
	}
	return Session{ID: m.SessionID()}, nil
}

func (m *Muse) Interrupt(context.Context, string) error {
	if m.TypeTerm != nil {
		return m.TypeTerm("/stop")
	}
	if m.term != nil && m.term.CanType() {
		return m.term.Type("/stop")
	}
	return nil
}

func (m *Muse) Events(context.Context, string) (<-chan Event, error) {
	return nil, fmt.Errorf("use Poll")
}

func (m *Muse) SessionID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionID
}

func (m *Muse) Workspace() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.workspace
}

func (m *Muse) Bind(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionID = id
	if id != "" {
		if p := findMuseSessionLog(m.sessionsRootLocked(), id); p != "" {
			m.logPath = p
		}
	}
}

func (m *Muse) beginSnapshot() {
	if m.snapDone == nil {
		m.snapDone = make(chan struct{})
	}
	go func() {
		defer m.snapOnce.Do(func() { close(m.snapDone) })
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_ = m.snapshotIDs(ctx)
	}()
}

func (m *Muse) waitSnapshot(ctx context.Context) {
	if m.snapDone == nil {
		return
	}
	select {
	case <-ctx.Done():
	case <-m.snapDone:
	}
}

func (m *Muse) Discover(ctx context.Context) (string, bool) {
	if id := m.SessionID(); id != "" {
		return id, true
	}
	m.waitSnapshot(ctx)
	rows, err := scanMuseSessions(m.sessionsRoot())
	if err != nil {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := pickMuseDiscover(rows, m.seenIDs, m.started)
	if !ok {
		return "", false
	}
	m.sessionID = id
	m.seenIDs[id] = true
	if p := findMuseSessionLog(m.sessionsRootLocked(), id); p != "" {
		m.logPath = p
	}
	return id, true
}

func (m *Muse) Inject(ctx context.Context, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("empty recovery prompt")
	}
	if m.TypeTerm != nil {
		return m.TypeTerm(prompt)
	}
	if m.term != nil && m.term.CanType() {
		return m.term.Type(prompt)
	}
	id := m.SessionID()
	if id == "" {
		var ok bool
		id, ok = m.Discover(ctx)
		if !ok {
			return fmt.Errorf("muse session not bound yet")
		}
	}
	_, err := m.run(ctx, m.bin, "exec", "--session-id", id, prompt)
	return err
}

func (m *Muse) Queue(ings ...Ingest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = append(m.queue, ings...)
}

func (m *Muse) Poll(ctx context.Context) ([]Ingest, error) {
	if m.SessionID() == "" {
		m.Discover(ctx)
	}
	id := m.SessionID()
	m.mu.Lock()
	queued := m.queue
	m.queue = nil
	m.mu.Unlock()
	if id == "" && len(queued) == 0 {
		return queued, nil
	}
	out := append([]Ingest(nil), queued...)
	if id == "" {
		return out, nil
	}
	msgs, err := m.pollLog(ctx, id)
	if err != nil {
		return out, err
	}
	return append(out, msgs...), nil
}

func (m *Muse) snapshotIDs(context.Context) error {
	ids, err := m.listSessions()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seenIDs == nil {
		m.seenIDs = map[string]bool{}
	}
	for _, id := range ids {
		m.seenIDs[id] = true
	}
	return nil
}

func (m *Muse) listSessions() ([]string, error) {
	return listMuseSessionIDs(m.sessionsRoot())
}

func (m *Muse) pollLog(ctx context.Context, id string) ([]Ingest, error) {
	path := m.sessionLogPath(id)
	if path != "" {
		raw, err := os.ReadFile(path)
		if err == nil {
			m.mu.Lock()
			cur := m.logCur
			m.mu.Unlock()
			evs, next := parseMuseJSONL(string(raw), cur)
			m.mu.Lock()
			m.logCur = next
			m.mu.Unlock()
			return evs, nil
		}
	}
	// Fallback: muse export --session <id> (writes path to stdout when --out omitted;
	// use --out - is not supported; write to temp via Exec capturing if export prints JSON).
	out, err := m.run(ctx, m.bin, "export", "--session", id)
	if err != nil {
		return nil, err
	}
	body := strings.TrimSpace(out)
	// Export without --out may print a path; try reading that file.
	if !strings.HasPrefix(body, "{") {
		if data, rerr := os.ReadFile(strings.TrimSpace(strings.Split(body, "\n")[0])); rerr == nil {
			body = string(data)
		}
	}
	m.mu.Lock()
	cur := m.logCur
	m.mu.Unlock()
	evs, next := parseMuseExport(body, cur)
	m.mu.Lock()
	m.logCur = next
	m.mu.Unlock()
	return evs, nil
}

func (m *Muse) sessionLogPath(id string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.logPath != "" && strings.Contains(m.logPath, id) {
		return m.logPath
	}
	p := findMuseSessionLog(m.sessionsRootLocked(), id)
	m.logPath = p
	return p
}

func (m *Muse) sessionsRoot() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionsRootLocked()
}

func (m *Muse) sessionsRootLocked() string {
	if m.SessionsRoot != "" {
		return m.SessionsRoot
	}
	return museSessionsRoot()
}

func (m *Muse) run(ctx context.Context, name string, args ...string) (string, error) {
	fn := m.Exec
	if fn == nil {
		fn = defaultExec
	}
	if name == "" {
		name = m.Command
	}
	return fn(ctx, name, args)
}

func (m *Muse) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func museSessionsRoot() string {
	if d := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); d != "" {
		return filepath.Join(d, "muse", "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".local", "share", "muse", "sessions")
	}
	return filepath.Join(home, ".local", "share", "muse", "sessions")
}
