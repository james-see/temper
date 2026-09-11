package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SessionPick is one attachable live session from Cursor, Hermes, or OpenCode.
type SessionPick struct {
	Agent     string
	SessionID string
	Workspace string
	Label     string
	Preview   string
	ModTime   time.Time
}

// ToCursorPick converts a cursor SessionPick for legacy call sites.
func (p SessionPick) ToCursorPick() CursorPick {
	return CursorPick{
		SessionID: p.SessionID,
		Workspace: p.Workspace,
		Label:     p.Label,
		Preview:   p.Preview,
		ModTime:   p.ModTime,
	}
}

// CursorToSessionPick wraps a CursorPick as a SessionPick.
func CursorToSessionPick(p CursorPick) SessionPick {
	return SessionPick{
		Agent:     "cursor",
		SessionID: p.SessionID,
		Workspace: p.Workspace,
		Label:     p.Label,
		Preview:   p.Preview,
		ModTime:   p.ModTime,
	}
}

const liveProbeTimeout = 3 * time.Second

// Probe hooks (overridable in tests).
var (
	probeCursor   = liveCursorSessionPicks
	probeHermes   = liveHermesPicks
	probeOpenCode = liveOpenCodePicks
)

// LiveCursorSessionPicks lists Cursor composers as SessionPicks.
func LiveCursorSessionPicks() ([]SessionPick, error) {
	return liveCursorSessionPicks()
}

func liveCursorSessionPicks() ([]SessionPick, error) {
	cps, err := LiveCursorPicks()
	if err != nil {
		return nil, err
	}
	out := make([]SessionPick, 0, len(cps))
	for _, p := range cps {
		out = append(out, CursorToSessionPick(p))
	}
	return out, nil
}

// LiveHermesPicks lists Hermes sessions that currently hold a live process lease.
// Historical rows from `hermes sessions list` are ignored — only ~/.hermes/runtime/active_sessions.json
// entries whose PID is still alive are returned.
func LiveHermesPicks(ctx context.Context) ([]SessionPick, error) {
	return liveHermesPicks(ctx)
}

func liveHermesPicks(ctx context.Context) ([]SessionPick, error) {
	_ = ctx
	entries, err := readHermesActiveSessions(hermesHome())
	if err != nil {
		return nil, err
	}
	var picks []SessionPick
	seen := map[string]bool{}
	for _, e := range entries {
		if !pidAlive(e.PID) {
			continue
		}
		id := strings.TrimSpace(e.SessionID)
		if id == "" {
			id = strings.TrimSpace(e.LiveSessionID)
		}
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		label := id
		if e.Surface != "" {
			label = e.Surface + " · " + shortSessionLabel(id)
		}
		p := SessionPick{
			Agent: "hermes", SessionID: id, Label: label, Preview: e.Surface,
		}
		if e.UpdatedAt > 0 {
			p.ModTime = time.Unix(int64(e.UpdatedAt), 0)
		}
		picks = append(picks, p)
	}
	return picks, nil
}

type hermesActiveEntry struct {
	SessionID     string
	LiveSessionID string
	PID           int
	Surface       string
	UpdatedAt     float64
}

func hermesHome() string {
	if v := strings.TrimSpace(os.Getenv("HERMES_HOME")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".hermes")
}

func readHermesActiveSessions(home string) ([]hermesActiveEntry, error) {
	if home == "" {
		return nil, fmt.Errorf("hermes home unknown")
	}
	path := filepath.Join(home, "runtime", "active_sessions.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseHermesActiveSessions(raw)
}

func parseHermesActiveSessions(raw []byte) ([]hermesActiveEntry, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	var root struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	var out []hermesActiveEntry
	for _, m := range root.Entries {
		e := hermesActiveEntry{
			SessionID:     firstString(m, "session_id", "sessionID"),
			LiveSessionID: "",
			Surface:       firstString(m, "surface"),
		}
		if meta, ok := m["metadata"].(map[string]any); ok {
			e.LiveSessionID = firstString(meta, "live_session_id", "session_id")
		}
		if n, ok := asFloat(m["pid"]); ok {
			e.PID = int(n)
		}
		if n, ok := asFloat(m["updated_at"]); ok {
			e.UpdatedAt = n
		} else if n, ok := asFloat(m["started_at"]); ok {
			e.UpdatedAt = n
		}
		out = append(out, e)
	}
	return out, nil
}

func pidAlive(pid int) bool {
	if pidAliveFn != nil {
		return pidAliveFn(pid)
	}
	return pidAliveOS(pid)
}

var pidAliveFn func(int) bool

func pidAliveOS(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; Signal(0) probes liveness.
	err = p.Signal(syscall.Signal(0))
	return err == nil
}

// LiveOpenCodePicks lists sessions on currently listening OpenCode servers.
// Disk history from `opencode session list` is ignored — no live server means no picks.
func LiveOpenCodePicks(ctx context.Context) ([]SessionPick, error) {
	return liveOpenCodePicks(ctx)
}

func liveOpenCodePicks(ctx context.Context) ([]SessionPick, error) {
	addrs, err := findOpenCodeListenAddrs(ctx)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, nil
	}
	var picks []SessionPick
	seen := map[string]bool{}
	for _, addr := range addrs {
		body, err := fetchOpenCodeSessions(ctx, addr)
		if err != nil {
			continue
		}
		for _, p := range parseOpenCodeSessionPicks(body) {
			if seen[p.SessionID] {
				continue
			}
			seen[p.SessionID] = true
			picks = append(picks, p)
		}
	}
	return picks, nil
}

var (
	findOpenCodeListenAddrsFn = findOpenCodeListenAddrsOS
	fetchOpenCodeSessionsFn   = fetchOpenCodeSessionsHTTP
)

func findOpenCodeListenAddrs(ctx context.Context) ([]string, error) {
	return findOpenCodeListenAddrsFn(ctx)
}

func fetchOpenCodeSessions(ctx context.Context, addr string) (string, error) {
	return fetchOpenCodeSessionsFn(ctx, addr)
}

func findOpenCodeListenAddrsOS(ctx context.Context) ([]string, error) {
	out, err := defaultExec(ctx, "lsof", []string{"-nP", "-iTCP", "-sTCP:LISTEN"})
	if err != nil {
		// No lsof / permission — treat as no live servers.
		return nil, nil
	}
	return filterOpenCodeTUIAddrs(ctx, parseOpenCodeListenPIDAddrs(out)), nil
}

type listenPIDAddr struct {
	PID  int
	Addr string
}

func parseOpenCodeListenPIDAddrs(out string) []listenPIDAddr {
	var rows []listenPIDAddr
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		cmd := strings.ToLower(fields[0])
		if cmd != "opencode" && !strings.HasPrefix(cmd, "opencode") {
			continue
		}
		pid := 0
		fmt.Sscanf(fields[1], "%d", &pid)
		name := ""
		for i := len(fields) - 1; i >= 0; i-- {
			if strings.Contains(fields[i], ":") && !strings.HasPrefix(fields[i], "(") {
				name = fields[i]
				break
			}
		}
		host, port, ok := strings.Cut(name, ":")
		if !ok || port == "" {
			continue
		}
		port = strings.TrimSuffix(port, "(LISTEN)")
		if host == "*" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		addr := host + ":" + port
		if seen[addr] {
			continue
		}
		seen[addr] = true
		rows = append(rows, listenPIDAddr{PID: pid, Addr: addr})
	}
	return rows
}

func parseOpenCodeListenAddrs(out string) []string {
	rows := parseOpenCodeListenPIDAddrs(out)
	addrs := make([]string, 0, len(rows))
	for _, r := range rows {
		addrs = append(addrs, r.Addr)
	}
	return addrs
}

func filterOpenCodeTUIAddrs(ctx context.Context, rows []listenPIDAddr) []string {
	var addrs []string
	for _, r := range rows {
		if r.PID > 0 && openCodeProcessIsACP(ctx, r.PID) {
			continue // Obsidian/editor ACP bridges are not Temper attach targets.
		}
		addrs = append(addrs, r.Addr)
	}
	return addrs
}

func openCodeProcessIsACP(ctx context.Context, pid int) bool {
	out, err := defaultExec(ctx, "ps", []string{"-p", fmt.Sprintf("%d", pid), "-o", "args="})
	if err != nil {
		return false
	}
	args := strings.ToLower(out)
	// Match "opencode acp" / ".../opencode acp --cwd ..."
	return strings.Contains(args, " acp") || strings.HasSuffix(strings.TrimSpace(args), "acp") ||
		strings.Contains(args, "/opencode acp")
}

func fetchOpenCodeSessionsHTTP(ctx context.Context, addr string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/session", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("opencode %s: %s", addr, resp.Status)
	}
	return string(body), nil
}

// LiveSessionPicks probes Cursor/Hermes/OpenCode in parallel.
// filterAgent limits to one adapter type when non-empty (e.g. "opencode").
// Missing binaries / Cursor-not-running yield empty groups, not errors.
func LiveSessionPicks(ctx context.Context, filterAgent string) ([]SessionPick, error) {
	filter := TypeOf(filterAgent)
	if filter == "" && filterAgent != "" {
		filter = Normalize(filterAgent)
	}
	type result struct {
		picks []SessionPick
		err   error
	}
	want := func(agent string) bool {
		return filter == "" || filter == agent
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var out []SessionPick
	var cursorErr error

	run := func(agent string, fn func(context.Context) ([]SessionPick, error)) {
		if !want(agent) {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, liveProbeTimeout)
			defer cancel()
			picks, err := fn(cctx)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if agent == "cursor" && filter == "cursor" {
					cursorErr = err
				}
				return
			}
			out = append(out, picks...)
		}()
	}

	run("cursor", func(context.Context) ([]SessionPick, error) { return probeCursor() })
	run("hermes", probeHermes)
	run("opencode", probeOpenCode)
	wg.Wait()

	if filter == "cursor" && len(out) == 0 && cursorErr != nil {
		return nil, cursorErr
	}
	return out, nil
}

var hermesAgeTail = regexp.MustCompile(`(?i)\s+(just\s+now|\d+\s*[mhd]\s*ago)\s*$`)

func parseHermesSessionPicks(out string) []SessionPick {
	var picks []SessionPick
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		m := sessionIDRe.FindStringSubmatch(line)
		if len(m) < 2 || seen[m[1]] {
			continue
		}
		id := m[1]
		seen[id] = true
		rest := strings.TrimSpace(line[:len(line)-len(strings.TrimSpace(m[0]))])
		rest = hermesAgeTail.ReplaceAllString(rest, "")
		label, ws := id, ""
		if fields := strings.Fields(rest); len(fields) >= 2 {
			ws = fields[len(fields)-1]
			label = strings.Join(fields[:len(fields)-1], " ")
		} else if len(fields) == 1 {
			label = fields[0]
		}
		p := SessionPick{Agent: "hermes", SessionID: id, Workspace: ws, Label: label}
		if t, ok := sessionStartedAt(id); ok {
			p.ModTime = t
		}
		picks = append(picks, p)
	}
	return picks
}

func parseOpenCodeSessionPicks(out string) []SessionPick {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	var root any
	if err := json.Unmarshal([]byte(out), &root); err != nil {
		// Fall back to ID-only table parse.
		var picks []SessionPick
		for _, id := range parseOpenCodeSessionIDsTable(out) {
			picks = append(picks, SessionPick{Agent: "opencode", SessionID: id, Label: shortSessionLabel(id)})
		}
		return picks
	}
	var rows []any
	switch v := root.(type) {
	case []any:
		rows = v
	case map[string]any:
		if arr, ok := v["sessions"].([]any); ok {
			rows = arr
		} else {
			rows = []any{v}
		}
	}
	var picks []SessionPick
	seen := map[string]bool{}
	for _, item := range rows {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := firstString(m, "id", "sessionID", "session_id")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		label := firstString(m, "title", "name", "label")
		if label == "" {
			label = shortSessionLabel(id)
		}
		ws := firstString(m, "directory", "workspace", "path")
		if loc, ok := m["location"].(map[string]any); ok {
			if ws == "" {
				ws = firstString(loc, "directory", "path")
			}
		}
		preview := firstString(m, "preview", "summary")
		mod := openCodeModTime(m)
		picks = append(picks, SessionPick{
			Agent: "opencode", SessionID: id, Workspace: ws, Label: label, Preview: preview, ModTime: mod,
		})
	}
	return picks
}

func openCodeModTime(m map[string]any) time.Time {
	if t, ok := m["time"].(map[string]any); ok {
		if n, ok := asFloat(t["updated"]); ok && n > 0 {
			return unixMaybeMs(n)
		}
		if n, ok := asFloat(t["created"]); ok && n > 0 {
			return unixMaybeMs(n)
		}
	}
	if n, ok := asFloat(m["updated"]); ok && n > 0 {
		return unixMaybeMs(n)
	}
	return time.Time{}
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func unixMaybeMs(n float64) time.Time {
	if n > 1e12 {
		return time.UnixMilli(int64(n))
	}
	return time.Unix(int64(n), 0)
}

func shortSessionLabel(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

// WorkspaceLabel returns a short display label for a path.
func WorkspaceLabel(ws string) string {
	base := filepath.Base(ws)
	if base == "" || base == "." {
		return ws
	}
	return base
}
