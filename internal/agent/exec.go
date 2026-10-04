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

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/term"
)

// Exec supervises an unsupported agent CLI as a Temper sidecar, driven
// entirely by a temper.yaml entry:
//
//	agents:
//	  aider:
//	    type: exec
//	    command: aider
//	    args: ["--no-auto-commits", "{prompt}"]
//	    resume_args: ["--restore", "{session}"]
//	    inject_args: ["--message", "{prompt}"]
//	    log_file: "~/.aider/history/{session}.jsonl"
//
// Placeholders {prompt}, {session}, {model}, {workspace} are substituted when
// argv is rendered. Exec has no live-session discovery: sessions bind
// explicitly via Start/Resume, and Poll tails LogFile when configured.
type Exec struct {
	Name    string
	Spec    config.Agent
	Command string
	Spawn   bool

	LookPath LookPathFunc
	Exec     ExecFunc
	OpenTerm OpenTermFunc
	TypeTerm TypeTermFunc
	Now      func() time.Time

	term *term.Handle

	mu        sync.Mutex
	bin       string
	workspace string
	model     string
	sessionID string
	argv      []string
	started   time.Time
	queue     []Ingest
	logLines  int
}

// NewExec constructs a generic exec sidecar. name is the temper.yaml agent id.
func NewExec(name string, spec config.Agent, spawn bool) *Exec {
	cmd := strings.TrimSpace(spec.Command)
	if cmd == "" {
		cmd = strings.TrimSpace(name)
	}
	return &Exec{
		Name:     Normalize(name),
		Spec:     spec,
		Command:  cmd,
		Spawn:    spawn,
		LookPath: exec.LookPath,
		Exec:     defaultExec,
		OpenTerm: term.Open,
		Now:      time.Now,
	}
}

func (e *Exec) ID() string {
	if e.Name != "" {
		return e.Name
	}
	return "exec"
}

func (e *Exec) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{
		ToolCalls:     e.Spec.Cap.Tools,
		Resume:        e.Spec.Cap.Resume || len(e.Spec.ResumeArgs) > 0,
		ModelOverride: e.Spec.Cap.ModelOverride,
	}, nil
}

func (e *Exec) Start(_ context.Context, req TaskRequest) (Session, error) {
	look := e.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look(e.Command)
	if err != nil {
		return Session{}, fmt.Errorf("%s not on PATH: %w", e.Command, err)
	}
	e.bin = bin
	e.workspace = req.Workspace
	if e.workspace == "" {
		e.workspace, _ = os.Getwd()
	}
	e.model = req.Model
	if req.Session != "" {
		e.Bind(req.Session)
	}
	e.started = e.now()
	e.argv = RenderExecCommand(e.Spec, e.Command, req.Prompt, e.SessionID(), req.Model, e.workspace)
	if e.Spawn {
		open := e.OpenTerm
		if open == nil {
			open = term.Open
		}
		handle, err := open(e.argv, e.workspace)
		if err != nil {
			return Session{}, err
		}
		e.term = handle
		if e.TypeTerm == nil && handle.CanType() {
			e.TypeTerm = handle.Type
		}
	}
	return Session{ID: req.RunID}, nil
}

func (e *Exec) LaunchArgs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.argv...)
}

func (e *Exec) LaunchLine() string {
	return term.CommandLine(e.LaunchArgs(), e.workspace)
}

// RenderExecCommand renders the launch argv for an exec spec. When session is
// non-empty and ResumeArgs are configured they win; otherwise Args are used.
func RenderExecCommand(spec config.Agent, command, prompt, session, model, workspace string) []string {
	tmpl := spec.Args
	if strings.TrimSpace(session) != "" && len(spec.ResumeArgs) > 0 {
		tmpl = spec.ResumeArgs
	}
	vars := map[string]string{
		"prompt":    prompt,
		"session":   session,
		"model":     model,
		"workspace": workspace,
	}
	out := []string{command}
	for _, a := range tmpl {
		out = append(out, renderExecVars(a, vars))
	}
	return out
}

func renderExecVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{"+k+"}", v)
	}
	return s
}

func (e *Exec) Resume(_ context.Context, id string) (Session, error) {
	if id != "" {
		e.Bind(id)
	}
	return Session{ID: e.SessionID()}, nil
}

func (e *Exec) Interrupt(context.Context, string) error {
	// Escape interrupts the current turn in most agent TUIs.
	esc := "\x1b"
	if e.TypeTerm != nil {
		return e.TypeTerm(esc)
	}
	if e.term != nil && e.term.CanType() {
		return e.term.Type(esc)
	}
	return nil
}

func (e *Exec) Events(context.Context, string) (<-chan Event, error) {
	return nil, fmt.Errorf("use Poll")
}

func (e *Exec) SessionID() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sessionID
}

func (e *Exec) Workspace() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.workspace
}

func (e *Exec) Bind(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sessionID = id
}

// Discover has no live source for generic CLIs; it reports the bound session.
func (e *Exec) Discover(context.Context) (string, bool) {
	if id := e.SessionID(); id != "" {
		return id, true
	}
	return "", false
}

func (e *Exec) Inject(ctx context.Context, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("empty recovery prompt")
	}
	if e.TypeTerm != nil {
		return e.TypeTerm(prompt)
	}
	if e.term != nil && e.term.CanType() {
		return e.term.Type(prompt)
	}
	if len(e.Spec.InjectArgs) == 0 {
		return fmt.Errorf("exec %s: no TUI to type into and no inject_args configured", e.ID())
	}
	vars := map[string]string{
		"prompt":    prompt,
		"session":   e.SessionID(),
		"model":     e.model,
		"workspace": e.Workspace(),
	}
	var args []string
	for _, a := range e.Spec.InjectArgs {
		args = append(args, renderExecVars(a, vars))
	}
	_, err := e.run(ctx, e.bin, args...)
	return err
}

func (e *Exec) Queue(ings ...Ingest) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.queue = append(e.queue, ings...)
}

func (e *Exec) Poll(context.Context) ([]Ingest, error) {
	e.mu.Lock()
	queued := e.queue
	e.queue = nil
	e.mu.Unlock()
	out := append([]Ingest(nil), queued...)
	lines, err := e.pollLog()
	if err != nil {
		return out, err
	}
	return append(out, lines...), nil
}

// pollLog tails the configured transcript; every new non-empty line becomes a
// model.completed ingest. Generic CLIs have no shared transcript schema, so
// the raw line is preserved as content for Reflex text heuristics.
func (e *Exec) pollLog() ([]Ingest, error) {
	path := e.logPath()
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
	lines := splitNonEmpty(string(raw))
	e.mu.Lock()
	n := e.logLines
	if n > len(lines) {
		n = 0
	}
	e.logLines = len(lines)
	e.mu.Unlock()
	var out []Ingest
	for _, ln := range lines[n:] {
		out = append(out, Ingest{Type: "model.completed", Data: map[string]any{"content": clip(strings.TrimSpace(ln), 4000)}})
	}
	return out, nil
}

func (e *Exec) logPath() string {
	tmpl := strings.TrimSpace(e.Spec.LogFile)
	if tmpl == "" {
		return ""
	}
	vars := map[string]string{
		"session":   e.SessionID(),
		"model":     e.model,
		"workspace": e.Workspace(),
		"prompt":    "",
	}
	p := renderExecVars(tmpl, vars)
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
	}
	return p
}

func (e *Exec) run(ctx context.Context, name string, args ...string) (string, error) {
	fn := e.Exec
	if fn == nil {
		fn = defaultExec
	}
	if name == "" {
		name = e.Command
	}
	return fn(ctx, name, args)
}

func (e *Exec) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}
