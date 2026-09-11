package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/james-see/temper/internal/term"
)

// HTTPDoFunc is an injectable HTTP client for tests.
type HTTPDoFunc func(req *http.Request) (*http.Response, error)

// OpenCode supervises the OpenCode CLI/TUI as a Temper sidecar.
type OpenCode struct {
	Command  string
	Spawn    bool
	LookPath LookPathFunc
	Exec     ExecFunc
	OpenTerm OpenTermFunc
	TypeTerm TypeTermFunc
	HTTPDo   HTTPDoFunc
	Now      func() time.Time
	// FreePort picks a local TCP port for the spawned TUI server (tests inject).
	FreePort func() (int, error)

	term *term.Handle

	mu        sync.Mutex
	bin       string
	workspace string
	sessionID string
	seenIDs   map[string]bool
	exportCur ocCursor
	argv      []string
	port      int
	baseURL   string
	started   time.Time
	queue     []Ingest
	snapOnce  sync.Once
	snapDone  chan struct{}
}

func NewOpenCode(command string, spawn bool) *OpenCode {
	if command == "" {
		command = "opencode"
	}
	return &OpenCode{
		Command:  command,
		Spawn:    spawn,
		LookPath: exec.LookPath,
		Exec:     defaultExec,
		OpenTerm: term.Open,
		HTTPDo:   http.DefaultClient.Do,
		Now:      time.Now,
		FreePort: freeLocalPort,
		seenIDs:  map[string]bool{},
		snapDone: make(chan struct{}),
	}
}

func (o *OpenCode) ID() string { return "opencode" }

func (o *OpenCode) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{ToolCalls: true, Resume: true, ModelOverride: true}, nil
}

func (o *OpenCode) Start(_ context.Context, req TaskRequest) (Session, error) {
	look := o.LookPath
	if look == nil {
		look = exec.LookPath
	}
	bin, err := look(o.Command)
	if err != nil {
		return Session{}, fmt.Errorf("opencode not on PATH: %w", err)
	}
	o.bin = bin
	o.workspace = req.Workspace
	if o.workspace == "" {
		o.workspace, _ = os.Getwd()
	}
	if req.Session != "" {
		o.Bind(req.Session)
	}
	o.started = o.now()
	port := 0
	if o.Spawn {
		fp := o.FreePort
		if fp == nil {
			fp = freeLocalPort
		}
		port, err = fp()
		if err != nil {
			return Session{}, fmt.Errorf("opencode port: %w", err)
		}
		o.mu.Lock()
		o.port = port
		o.baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
		o.mu.Unlock()
	}
	o.argv = openCodeLaunchArgs(o.workspace, req.Prompt, port)
	o.beginSnapshot()
	if o.Spawn {
		open := o.OpenTerm
		if open == nil {
			open = term.Open
		}
		handle, err := open(o.argv, o.workspace)
		if err != nil {
			return Session{}, err
		}
		o.term = handle
		if o.TypeTerm == nil && handle.CanType() {
			o.TypeTerm = handle.Type
		}
	}
	return Session{ID: req.RunID}, nil
}

func (o *OpenCode) LaunchArgs() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.argv...)
}

func (o *OpenCode) LaunchLine() string {
	return term.CommandLine(o.LaunchArgs(), o.workspace)
}

func openCodeLaunchArgs(workspace, seed string, port int) []string {
	args := []string{"opencode"}
	if port > 0 {
		args = append(args, "--port", strconv.Itoa(port))
	}
	if strings.TrimSpace(seed) != "" {
		args = append(args, "--prompt", seed)
	}
	_ = workspace // cwd is set by term.Open; keep signature parallel to Hermes.
	return args
}

func (o *OpenCode) Resume(_ context.Context, id string) (Session, error) {
	if id != "" {
		o.Bind(id)
	}
	return Session{ID: o.SessionID()}, nil
}

func (o *OpenCode) Interrupt(ctx context.Context, id string) error {
	if id == "" {
		id = o.SessionID()
	}
	if id == "" || o.serverURL() == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.serverURL()+"/session/"+id+"/abort", nil)
	if err != nil {
		return err
	}
	resp, err := o.doHTTP(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func (o *OpenCode) Events(context.Context, string) (<-chan Event, error) {
	return nil, fmt.Errorf("use Poll")
}

func (o *OpenCode) SessionID() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.sessionID
}

func (o *OpenCode) Workspace() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.workspace
}

func (o *OpenCode) Bind(id string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessionID = id
}

func (o *OpenCode) Port() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.port
}

func (o *OpenCode) serverURL() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.baseURL
}

func (o *OpenCode) beginSnapshot() {
	if o.snapDone == nil {
		o.snapDone = make(chan struct{})
	}
	go func() {
		defer o.snapOnce.Do(func() { close(o.snapDone) })
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_ = o.snapshotIDs(ctx)
	}()
}

func (o *OpenCode) waitSnapshot(ctx context.Context) {
	if o.snapDone == nil {
		return
	}
	select {
	case <-ctx.Done():
	case <-o.snapDone:
	}
}

func (o *OpenCode) Discover(ctx context.Context) (string, bool) {
	if id := o.SessionID(); id != "" {
		return id, true
	}
	o.waitSnapshot(ctx)
	rows, err := o.listSessions(ctx)
	if err != nil {
		return "", false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	id, ok := pickOpenCodeUnseen(rows, o.seenIDs)
	if !ok {
		return "", false
	}
	o.sessionID = id
	o.seenIDs[id] = true
	return id, true
}

func (o *OpenCode) Inject(ctx context.Context, prompt string) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return fmt.Errorf("empty recovery prompt")
	}
	if o.TypeTerm != nil {
		return o.TypeTerm(prompt)
	}
	if o.term != nil && o.term.CanType() {
		return o.term.Type(prompt)
	}
	if o.serverURL() != "" {
		if err := o.injectHTTP(ctx, prompt); err == nil {
			return nil
		}
	}
	id := o.SessionID()
	if id == "" {
		var ok bool
		id, ok = o.Discover(ctx)
		if !ok {
			return fmt.Errorf("opencode session not bound yet")
		}
	}
	_, err := o.run(ctx, o.bin, "run", "--session", id, "--format", "json", prompt)
	return err
}

func (o *OpenCode) injectHTTP(ctx context.Context, prompt string) error {
	base := o.serverURL()
	body, _ := json.Marshal(map[string]any{"text": prompt})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/tui/append-prompt", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.doHTTP(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("opencode append-prompt: %s", resp.Status)
	}
	req2, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/tui/submit-prompt", nil)
	if err != nil {
		return err
	}
	resp2, err := o.doHTTP(req2)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode >= 300 {
		return fmt.Errorf("opencode submit-prompt: %s", resp2.Status)
	}
	return nil
}

func (o *OpenCode) Queue(ings ...Ingest) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.queue = append(o.queue, ings...)
}

func (o *OpenCode) Poll(ctx context.Context) ([]Ingest, error) {
	if o.SessionID() == "" {
		o.Discover(ctx)
	}
	id := o.SessionID()
	o.mu.Lock()
	queued := o.queue
	o.queue = nil
	o.mu.Unlock()
	if id == "" && len(queued) == 0 {
		return queued, nil
	}
	out := append([]Ingest(nil), queued...)
	if id == "" {
		return out, nil
	}
	msgs, err := o.pollMessages(ctx, id)
	if err != nil {
		return out, err
	}
	return append(out, msgs...), nil
}

func (o *OpenCode) snapshotIDs(ctx context.Context) error {
	ids, err := o.listSessions(ctx)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seenIDs == nil {
		o.seenIDs = map[string]bool{}
	}
	for _, id := range ids {
		o.seenIDs[id] = true
	}
	return nil
}

func (o *OpenCode) listSessions(ctx context.Context) ([]string, error) {
	if base := o.serverURL(); base != "" {
		if ids, err := o.listSessionsHTTP(ctx, base); err == nil && len(ids) > 0 {
			return ids, nil
		}
	}
	out, err := o.run(ctx, o.bin, "session", "list", "--format", "json", "-n", "20")
	if err != nil {
		return nil, err
	}
	return parseOpenCodeSessionIDs(out), nil
}

func (o *OpenCode) listSessionsHTTP(ctx context.Context, base string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/session", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.doHTTP(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("opencode session list: %s", resp.Status)
	}
	return parseOpenCodeSessionIDs(string(body)), nil
}

func (o *OpenCode) pollMessages(ctx context.Context, id string) ([]Ingest, error) {
	if base := o.serverURL(); base != "" {
		ings, err := o.pollMessagesHTTP(ctx, base, id)
		if err == nil {
			return ings, nil
		}
	}
	out, err := o.run(ctx, o.bin, "export", id)
	if err != nil {
		return nil, err
	}
	o.mu.Lock()
	cur := o.exportCur
	o.mu.Unlock()
	evs, next := parseOpenCodeExport(out, cur)
	o.mu.Lock()
	o.exportCur = next
	o.mu.Unlock()
	return evs, nil
}

func (o *OpenCode) pollMessagesHTTP(ctx context.Context, base, id string) ([]Ingest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/session/"+id+"/message", nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.doHTTP(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("opencode messages: %s", resp.Status)
	}
	o.mu.Lock()
	cur := o.exportCur
	o.mu.Unlock()
	evs, next := parseOpenCodeExport(string(body), cur)
	o.mu.Lock()
	o.exportCur = next
	o.mu.Unlock()
	return evs, nil
}

func (o *OpenCode) run(ctx context.Context, name string, args ...string) (string, error) {
	fn := o.Exec
	if fn == nil {
		fn = defaultExec
	}
	if name == "" {
		name = o.Command
	}
	return fn(ctx, name, args)
}

func (o *OpenCode) doHTTP(req *http.Request) (*http.Response, error) {
	fn := o.HTTPDo
	if fn == nil {
		fn = http.DefaultClient.Do
	}
	return fn(req)
}

func (o *OpenCode) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func freeLocalPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("no tcp addr")
	}
	return addr.Port, nil
}
