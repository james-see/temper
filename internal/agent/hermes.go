package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/james-see/temper/internal/term"
)

var sessionIDRe = regexp.MustCompile(`(\d{8}_\d{6}_[a-f0-9]+)\s*$`)

type ExecFunc func(ctx context.Context, name string, args []string) (string, error)
type OpenTermFunc func(argv []string, dir string) error
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
	Now      func() time.Time

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
	}
}

func (h *Hermes) ID() string { return "hermes" }

func (h *Hermes) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{Streaming: true, ToolCalls: true, Resume: true, Subagents: true, ACP: true, ModelOverride: true}, nil
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
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := h.run(ctx, bin, "--version"); err != nil {
		return Session{}, fmt.Errorf("hermes --version: %w", err)
	}
	if err := h.snapshotIDs(context.Background()); err != nil {
		// listing can fail on a fresh install; still launch
		h.mu.Lock()
		h.seenIDs = map[string]bool{}
		h.mu.Unlock()
	}
	h.started = h.now()
	h.argv = hermesLaunchArgs(h.workspace, req.Prompt)
	if h.Spawn {
		open := h.OpenTerm
		if open == nil {
			open = term.Open
		}
		if err := open(h.argv, h.workspace); err != nil {
			return Session{}, err
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
	if strings.TrimSpace(seed) != "" {
		return []string{"hermes", "chat", "--tui", "--in", workspace, "--source", "temper", "-q", seed}
	}
	return []string{"hermes", "--tui", "--in", workspace, "--source", "temper"}
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

func (h *Hermes) Discover(ctx context.Context) (string, bool) {
	if id := h.SessionID(); id != "" {
		return id, true
	}
	rows, err := h.listSessions(ctx)
	if err != nil {
		return "", false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, id := range rows {
		if !h.seenIDs[id] {
			h.sessionID = id
			h.seenIDs[id] = true
			return id, true
		}
	}
	return "", false
}

func (h *Hermes) Inject(ctx context.Context, prompt string) error {
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
	msgs, err := h.pollExport(ctx, id)
	if err == nil {
		out = append(out, msgs...)
	}
	logs, err := h.pollLogs(ctx, id)
	if err == nil {
		out = append(out, logs...)
	}
	return out, nil
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
	args := []string{"sessions", "list", "--limit", "8"}
	if h.workspace != "" {
		args = append(args, "--workspace", filepath.Base(h.workspace))
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
	lines := splitNonEmpty(out)
	h.mu.Lock()
	start := h.exportSeen
	if start > len(lines) {
		start = 0
	}
	h.exportSeen = len(lines)
	h.mu.Unlock()
	var evs []Ingest
	for _, line := range lines[start:] {
		if ing, ok := parseExportLine(line); ok {
			evs = append(evs, ing)
		}
	}
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

func parseExportLine(line string) (Ingest, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Ingest{}, false
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Ingest{}, false
	}
	role := strings.ToLower(asString(raw["role"]))
	if role == "" {
		role = strings.ToLower(asString(raw["type"]))
	}
	text := firstString(raw, "content", "text", "message", "query")
	if text == "" {
		if m, ok := raw["message"].(map[string]any); ok {
			text = firstString(m, "content", "text")
			if role == "" {
				role = strings.ToLower(asString(m["role"]))
			}
		}
	}
	if strings.TrimSpace(text) == "" {
		return Ingest{}, false
	}
	switch role {
	case "user", "human":
		return Ingest{Type: "user.message", Data: map[string]any{"text": text}}, true
	case "assistant", "model", "agent", "ai":
		return Ingest{Type: "model.completed", Data: map[string]any{"content": text, "tool_calls": 0}}, true
	default:
		if strings.Contains(role, "user") {
			return Ingest{Type: "user.message", Data: map[string]any{"text": text}}, true
		}
		return Ingest{Type: "model.completed", Data: map[string]any{"content": text, "tool_calls": 0}}, true
	}
}

func classifyLogLine(line string) (Ingest, bool) {
	low := strings.ToLower(line)
	if strings.Contains(low, "http request") || strings.Contains(low, "plugin ") {
		return Ingest{}, false
	}
	if strings.Contains(low, "error") || strings.Contains(low, "traceback") {
		return Ingest{Type: "tool.completed", Data: map[string]any{"tool": "hermes", "error": clip(line, 400)}}, true
	}
	if strings.Contains(low, "tool") && (strings.Contains(low, "call") || strings.Contains(low, "run") || strings.Contains(low, "exec")) {
		return Ingest{Type: "tool.requested", Data: map[string]any{"tool": "hermes", "args": clip(line, 400)}}, true
	}
	if strings.Contains(low, "generat") || strings.Contains(low, "completion") || strings.Contains(low, "model") {
		return Ingest{Type: "model.called", Data: map[string]any{"line": clip(line, 240)}}, true
	}
	return Ingest{}, false
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
