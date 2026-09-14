package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type claudeCursor struct {
	n int // lines consumed
}

type claudeSessionRow struct {
	ID      string
	LogPath string
	ModTime time.Time
}

const claudeLiveWindow = 30 * time.Minute

// encodeClaudeProjectDir mirrors Claude Code's project directory naming:
// non-alphanumeric runes become '-', and names longer than 200 chars are
// truncated with a hash suffix.
func encodeClaudeProjectDir(cwd string) string {
	if named := strings.TrimSpace(os.Getenv("CLAUDE_CODE_PROJECT_DIR_NAME")); named != "" &&
		strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")) != "" {
		if ok, _ := regexp.MatchString(`^[A-Za-z0-9_-]{1,64}$`, named); ok {
			return named
		}
	}
	abs := cwd
	if a, err := filepath.Abs(cwd); err == nil {
		abs = a
	}
	var b strings.Builder
	for _, r := range abs {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	name := b.String()
	if len(name) <= 200 {
		return name
	}
	sum := sha256.Sum256([]byte(abs))
	return name[:200] + "-" + hex.EncodeToString(sum[:8])
}

// parseClaudeJSONL maps Claude Code transcript JSONL into Temper ingests.
// Capture a real fixture later with:
//
//	CLAUDE_CONFIG_DIR=/tmp/cc claude -p --output-format json "hi"
//
// then copy ~/.claude/projects/.../<id>.jsonl (or $CLAUDE_CONFIG_DIR/...).
func parseClaudeJSONL(out string, cur claudeCursor) ([]Ingest, claudeCursor) {
	lines := splitNonEmpty(out)
	if len(lines) == 0 {
		return nil, cur
	}
	if cur.n > len(lines) {
		cur.n = 0
	}
	var evs []Ingest
	for _, line := range lines[cur.n:] {
		evs = append(evs, ingestClaudeLine(line)...)
	}
	cur.n = len(lines)
	return evs, cur
}

func ingestClaudeLine(line string) []Ingest {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil
	}
	return ingestClaudeMessage(raw)
}

func ingestClaudeMessage(raw map[string]any) []Ingest {
	typ := strings.ToLower(firstString(raw, "type", "role"))
	// Nested message object (stream-json / transcript wrappers).
	if msg, ok := raw["message"].(map[string]any); ok {
		if typ == "" || typ == "user" || typ == "assistant" || typ == "system" {
			nested := ingestClaudeMessage(msg)
			if len(nested) > 0 {
				return nested
			}
		}
	}
	switch typ {
	case "system", "progress", "stream_event", "file-history-snapshot", "":
		return nil
	case "result":
		if text := firstString(raw, "result", "text", "content"); text != "" {
			return []Ingest{{Type: "model.completed", Data: map[string]any{"content": text}}}
		}
		return nil
	case "user", "human":
		text := claudeTextContent(raw)
		if text == "" {
			text = firstString(raw, "text", "content", "prompt")
		}
		// tool_result blocks often arrive as user messages.
		if results := claudeToolResults(raw); len(results) > 0 {
			return results
		}
		if text != "" {
			return []Ingest{{Type: "user.message", Data: map[string]any{"text": text}}}
		}
		return nil
	case "assistant", "ai", "model":
		var out []Ingest
		if reason := claudeThinkingContent(raw); reason != "" {
			out = append(out, thinkingIngest(len(reason), len(reason), nil, false, reason))
		}
		for _, tc := range claudeToolUses(raw) {
			out = append(out, Ingest{Type: "tool.requested", Data: map[string]any{"tool": tc.name, "args": tc.args}})
		}
		if text := claudeTextContent(raw); text != "" {
			out = append(out, Ingest{Type: "model.completed", Data: map[string]any{"content": text, "tool_calls": len(claudeToolUses(raw))}})
		}
		return out
	default:
		// Some transcripts omit type and only have role/content.
		if role := strings.ToLower(firstString(raw, "role")); role == "user" || role == "assistant" {
			raw["type"] = role
			return ingestClaudeMessage(raw)
		}
		return nil
	}
}

func claudeContentBlocks(raw map[string]any) []any {
	if arr, ok := raw["content"].([]any); ok {
		return arr
	}
	if msg, ok := raw["message"].(map[string]any); ok {
		if arr, ok := msg["content"].([]any); ok {
			return arr
		}
	}
	return nil
}

func claudeTextContent(raw map[string]any) string {
	if s := firstString(raw, "text"); s != "" {
		return s
	}
	if s, ok := raw["content"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	for _, item := range claudeContentBlocks(raw) {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(firstString(block, "type"))
		if kind != "text" && kind != "" {
			continue
		}
		if s := firstString(block, "text", "content"); s != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(s)
		}
	}
	return strings.TrimSpace(b.String())
}

func claudeThinkingContent(raw map[string]any) string {
	if s := firstString(raw, "thinking", "reasoning"); s != "" {
		return s
	}
	var b strings.Builder
	for _, item := range claudeContentBlocks(raw) {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(firstString(block, "type"))
		if kind != "thinking" && kind != "reasoning" {
			continue
		}
		if s := firstString(block, "thinking", "text", "content"); s != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(s)
		}
	}
	return strings.TrimSpace(b.String())
}

func claudeToolUses(raw map[string]any) []toolCall {
	var out []toolCall
	for _, item := range claudeContentBlocks(raw) {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(firstString(block, "type"))
		if kind != "tool_use" && kind != "tooluse" {
			continue
		}
		name := firstString(block, "name", "tool")
		args := firstString(block, "input", "arguments", "args")
		if args == "" {
			if inp, ok := block["input"]; ok {
				args = claudeArgsString(inp)
			}
		}
		if name == "" {
			continue
		}
		out = append(out, toolCall{name: name, args: args})
	}
	return out
}

func claudeToolResults(raw map[string]any) []Ingest {
	var out []Ingest
	for _, item := range claudeContentBlocks(raw) {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(firstString(block, "type"))
		if kind != "tool_result" && kind != "toolresult" {
			continue
		}
		name := firstString(block, "tool_use_id", "tool_name", "name", "tool")
		if name == "" {
			name = "claude"
		}
		content := firstString(block, "content", "output", "text")
		if content == "" {
			content = claudeArgsString(block["content"])
		}
		data := map[string]any{"tool": name, "output": content}
		if isErr, ok := block["is_error"].(bool); ok && isErr {
			data["error"] = clip(content, 400)
		}
		out = append(out, Ingest{Type: "tool.completed", Data: data})
	}
	return out
}

func claudeArgsString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

func pickClaudeUnseen(ids []string, seen map[string]bool) (string, bool) {
	for _, id := range ids {
		if !seen[id] {
			return id, true
		}
	}
	return "", false
}

func listClaudeSessionIDs(projectDir string) ([]string, error) {
	rows, err := scanClaudeSessions(projectDir)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func scanClaudeSessions(projectDir string) ([]claudeSessionRow, error) {
	if projectDir == "" {
		return nil, nil
	}
	info, err := os.Stat(projectDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, err
	}
	var rows []claudeSessionRow
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".jsonl")
		if id == "" {
			continue
		}
		mod := time.Time{}
		if fi, err := e.Info(); err == nil {
			mod = fi.ModTime()
		}
		rows = append(rows, claudeSessionRow{
			ID: id, LogPath: filepath.Join(projectDir, e.Name()), ModTime: mod,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].ModTime.Equal(rows[j].ModTime) {
			return rows[i].ModTime.After(rows[j].ModTime)
		}
		return rows[i].ID > rows[j].ID
	})
	return rows, nil
}

func findClaudeSessionLog(projectDir, id string) string {
	id = strings.TrimSpace(id)
	if id == "" || projectDir == "" {
		return ""
	}
	direct := filepath.Join(projectDir, id+".jsonl")
	if _, err := os.Stat(direct); err == nil {
		return direct
	}
	rows, err := scanClaudeSessions(projectDir)
	if err != nil {
		return ""
	}
	for _, r := range rows {
		if r.ID == id || strings.HasPrefix(r.ID, id) || strings.HasPrefix(id, r.ID) {
			return r.LogPath
		}
	}
	return ""
}

// LiveClaudePicks lists Claude Code sessions from `claude agents --json` and
// recent project transcripts (updated within claudeLiveWindow).
func LiveClaudePicks(ctx context.Context) ([]SessionPick, error) {
	return liveClaudePicks(ctx)
}

var (
	claudeAgentsJSONFn     = claudeAgentsJSONOS
	claudeProjectDirForCWD = claudeProjectDirDefault
)

func liveClaudePicks(ctx context.Context) ([]SessionPick, error) {
	var picks []SessionPick
	seen := map[string]bool{}

	if body, err := claudeAgentsJSONFn(ctx); err == nil {
		for _, p := range parseClaudeAgentsJSON(body) {
			if seen[p.SessionID] {
				continue
			}
			seen[p.SessionID] = true
			picks = append(picks, p)
		}
	}

	ws, _ := os.Getwd()
	dir := claudeProjectDirForCWD(ws)
	rows, err := scanClaudeSessions(dir)
	if err != nil {
		return picks, nil
	}
	cutoff := time.Now().Add(-claudeLiveWindow)
	for _, r := range rows {
		if seen[r.ID] {
			continue
		}
		if !r.ModTime.IsZero() && r.ModTime.Before(cutoff) {
			continue
		}
		seen[r.ID] = true
		picks = append(picks, SessionPick{
			Agent: "claude-code", SessionID: r.ID, Workspace: ws,
			Label: shortSessionLabel(r.ID), Preview: "claude", ModTime: r.ModTime,
		})
	}
	return picks, nil
}

func claudeAgentsJSONOS(ctx context.Context) (string, error) {
	return defaultExec(ctx, "claude", []string{"agents", "--json"})
}

func claudeProjectDirDefault(ws string) string {
	return filepath.Join(claudeConfigDir(), "projects", encodeClaudeProjectDir(ws))
}

// parseClaudeAgentsJSON parses `claude agents --json` output into SessionPicks.
func parseClaudeAgentsJSON(out string) []SessionPick {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	var root any
	if err := json.Unmarshal([]byte(out), &root); err != nil {
		return nil
	}
	var rows []any
	switch v := root.(type) {
	case []any:
		rows = v
	case map[string]any:
		if arr, ok := v["sessions"].([]any); ok {
			rows = arr
		} else if arr, ok := v["agents"].([]any); ok {
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
		id := firstString(m, "id", "session_id", "sessionId", "sessionID")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		label := firstString(m, "name", "title", "label", "display_name")
		if label == "" {
			label = shortSessionLabel(id)
		}
		ws := firstString(m, "cwd", "directory", "workspace", "path", "working_dir")
		preview := firstString(m, "status", "preview", "summary")
		if preview == "" {
			preview = "claude"
		}
		picks = append(picks, SessionPick{
			Agent: "claude-code", SessionID: id, Workspace: ws, Label: label, Preview: preview,
		})
	}
	return picks
}
