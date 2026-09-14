package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// gooseCursor tracks how much of a Goose session export has been consumed.
// n is the number of messages already emitted; lastID/thinkLen support
// thinking-growth detection on the last message, mirroring the Hermes cursor.
type gooseCursor struct {
	n        int
	lastID   string
	thinkLen int
}

// parseGooseExport consumes `goose session export --session-id <id>
// --format json` output: a Session object whose `conversation` field is a
// plain array of Message objects (Conversation is a tuple struct in Goose).
// It also tolerates the older wrapped shapes and JSONL transcripts.
func parseGooseExport(out string, cur gooseCursor) ([]Ingest, gooseCursor) {
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil, cur
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(trimmed), &raw); err == nil {
		msgs := gooseConversation(raw)
		if msgs != nil {
			return parseGooseMessages(msgs, cur)
		}
		return nil, cur
	}
	// Legacy JSONL transcript: one Message per line (possibly wrapped in "entry").
	lines := splitNonEmpty(out)
	if len(lines) == 0 {
		return nil, cur
	}
	start := cur.n
	if start > len(lines) {
		start = 0
	}
	var evs []Ingest
	for _, line := range lines[start:] {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		evs = append(evs, gooseMessage(m)...)
	}
	cur.n = len(lines)
	return evs, cur
}

// gooseConversation extracts the message array from a Session JSON object.
func gooseConversation(session map[string]any) []any {
	switch c := session["conversation"].(type) {
	case []any:
		return c
	case map[string]any:
		if msgs, ok := c["messages"].([]any); ok {
			return msgs
		}
	}
	return nil
}

func parseGooseMessages(msgs []any, cur gooseCursor) ([]Ingest, gooseCursor) {
	if cur.n > len(msgs) {
		cur.n = 0
	}
	var evs []Ingest
	grew := len(msgs) > cur.n
	for _, item := range msgs[cur.n:] {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		evs = append(evs, gooseMessage(raw)...)
	}
	if !grew {
		if ing, ok := gooseThinkGrowth(msgs, cur); ok {
			evs = append(evs, ing)
		}
	}
	cur.n = len(msgs)
	if len(msgs) > 0 {
		if last, ok := msgs[len(msgs)-1].(map[string]any); ok {
			cur.lastID = gooseMessageID(last)
			cur.thinkLen = gooseThinkLen(last)
		}
	}
	return evs, cur
}

func gooseThinkGrowth(msgs []any, cur gooseCursor) (Ingest, bool) {
	if len(msgs) == 0 {
		return Ingest{}, false
	}
	last, ok := msgs[len(msgs)-1].(map[string]any)
	if !ok {
		return Ingest{}, false
	}
	id := gooseMessageID(last)
	rlen := gooseThinkLen(last)
	if id == "" || id != cur.lastID || rlen <= cur.thinkLen {
		return Ingest{}, false
	}
	return thinkingIngest(rlen, rlen-cur.thinkLen, nil, gooseTextBlocks(last) != "", gooseThinkText(last)), true
}

func gooseMessageID(raw map[string]any) string {
	return firstString(raw, "id")
}

func gooseBlockOf(raw map[string]any) []any {
	blocks, _ := raw["content"].([]any)
	return blocks
}

// gooseThinkLen sums the characters of every thinking block in a message.
func gooseThinkLen(raw map[string]any) int {
	return len(gooseThinkText(raw))
}

func gooseThinkText(raw map[string]any) string {
	var b strings.Builder
	for _, item := range gooseBlockOf(raw) {
		block, ok := item.(map[string]any)
		if !ok || gooseBlockKind(block) != "thinking" {
			continue
		}
		if s := firstString(block, "thinking", "text"); s != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(s)
		}
	}
	return b.String()
}

// gooseTextBlocks concatenates text-block content of a message.
func gooseTextBlocks(raw map[string]any) string {
	var b strings.Builder
	for _, item := range gooseBlockOf(raw) {
		block, ok := item.(map[string]any)
		if !ok || gooseBlockKind(block) != "text" {
			continue
		}
		if s := firstString(block, "text"); s != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(s)
		}
	}
	return strings.TrimSpace(b.String())
}

func gooseBlockKind(block map[string]any) string {
	kind := firstString(block, "type")
	if kind == "" {
		return ""
	}
	return strings.ToLower(kind)
}

// gooseMessage converts one Goose Message (role + typed content blocks) into
// Temper ingests, mirroring the Hermes export semantics.
func gooseMessage(raw map[string]any) []Ingest {
	role := strings.ToLower(firstString(raw, "role"))
	// Legacy transcripts wrap the message in an "entry" object.
	if role == "" {
		if entry, ok := raw["entry"].(map[string]any); ok {
			return gooseMessage(entry)
		}
		role = strings.ToLower(firstString(raw, "type"))
	}
	switch role {
	case "system", "session_meta", "":
		return nil
	case "user", "human":
		if text := gooseTextBlocks(raw); text != "" {
			return []Ingest{{Type: "user.message", Data: map[string]any{"text": text}}}
		}
		return nil
	case "tool":
		name := firstString(raw, "tool_name", "name", "tool")
		if name == "" {
			name = "goose"
		}
		text := gooseTextBlocks(raw)
		data := map[string]any{"tool": name, "output": text}
		if err := firstString(raw, "error"); err != "" {
			data["error"] = err
		}
		return []Ingest{{Type: "tool.completed", Data: data}}
	case "assistant", "model", "agent", "ai":
		var out []Ingest
		var calls int
		for _, item := range gooseBlockOf(raw) {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch gooseBlockKind(block) {
			case "thinking":
				if reason := firstString(block, "thinking", "text"); reason != "" {
					out = append(out, thinkingIngest(len(reason), len(reason), nil, false, reason))
				}
			case "toolrequest", "tool_request":
				ing, ok := gooseToolRequestIngest(block)
				if ok {
					calls++
					out = append(out, ing)
				}
			case "toolresponse", "tool_response":
				out = append(out, gooseToolResponseIngest(block))
			case "error":
				if msg := firstString(block, "message"); msg != "" {
					out = append(out, Ingest{Type: "tool.completed", Data: map[string]any{
						"tool": "goose", "error": clip(msg, 400),
					}})
				}
			}
		}
		if text := gooseTextBlocks(raw); text != "" {
			out = append(out, Ingest{Type: "model.completed", Data: map[string]any{
				"content": text, "tool_calls": calls,
			}})
		}
		return out
	default:
		return nil
	}
}

// gooseToolRequestIngest extracts a tool call from a toolRequest content
// block. The params live under toolCall (camelCase) or tool_call (snake) and
// carry `name` plus `arguments`; extraction is tolerant about nesting.
func gooseToolRequestIngest(block map[string]any) (Ingest, bool) {
	call, ok := block["toolCall"].(map[string]any)
	if !ok {
		call, ok = block["tool_call"].(map[string]any)
	}
	name, args := gooseToolNameArgs(call)
	if name == "" {
		name, args = gooseToolNameArgs(block)
	}
	if name == "" {
		return Ingest{}, false
	}
	return Ingest{Type: "tool.requested", Data: map[string]any{"tool": name, "args": args}}, true
}

func gooseToolNameArgs(call map[string]any) (string, string) {
	if call == nil {
		return "", ""
	}
	if name := firstString(call, "name", "tool"); name != "" {
		return name, gooseArgsString(call["arguments"])
	}
	if params, ok := call["params"].(map[string]any); ok {
		if name := firstString(params, "name", "tool"); name != "" {
			return name, gooseArgsString(params["arguments"])
		}
	}
	return "", ""
}

func gooseArgsString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if b, err := json.Marshal(t); err == nil {
			return string(b)
		}
	}
	return ""
}

// gooseToolResponseIngest converts a toolResponse content block. The result
// carries MCP `content` items; text items are concatenated into the output
// and isError marks a failure.
func gooseToolResponseIngest(block map[string]any) Ingest {
	result, _ := block["toolResult"].(map[string]any)
	if result == nil {
		result, _ = block["tool_result"].(map[string]any)
	}
	data := map[string]any{"tool": firstString(block, "id"), "output": gooseResultText(result)}
	if data["tool"] == "" {
		data["tool"] = "goose"
	}
	if gooseResultIsError(result) {
		data["error"] = clip(strings.TrimSpace(data["output"].(string)+" "+firstString(result, "error")), 400)
	}
	return Ingest{Type: "tool.completed", Data: data}
}

func gooseResultText(result map[string]any) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	if items, ok := result["content"].([]any); ok {
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok || firstString(m, "type") != "text" {
				continue
			}
			if s := firstString(m, "text"); s != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(s)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func gooseResultIsError(result map[string]any) bool {
	if result == nil {
		return false
	}
	switch v := result["isError"].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	}
	if e := firstString(result, "error"); e != "" && e != "null" {
		return true
	}
	return false
}

// gooseSessionIDRe matches Goose session IDs, e.g. 20250921_143022.
var gooseSessionIDRe = regexp.MustCompile(`\b(\d{8}_\d{6})\b`)

// parseGooseSessionList consumes `goose session list --format json` output:
// {"totalSessions": N, "sessions": [{"id": "20250921_143022", ...}]}, with a
// plain-text table fallback. When workspace is non-empty, sessions created in
// other working directories are skipped.
func parseGooseSessionList(out string, workspace string) []string {
	var ids []string
	seen := map[string]bool{}
	var root struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &root); err == nil && root.Sessions != nil {
		for _, m := range root.Sessions {
			id := firstString(m, "id")
			if id == "" || seen[id] {
				continue
			}
			if !gooseSameWorkspace(firstString(m, "working_dir", "workingDir"), workspace) {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
		return ids
	}
	for _, line := range strings.Split(out, "\n") {
		m := gooseSessionIDRe.FindStringSubmatch(line)
		if len(m) < 2 || seen[m[1]] {
			continue
		}
		seen[m[1]] = true
		ids = append(ids, m[1])
	}
	return ids
}

func gooseSameWorkspace(a, b string) bool {
	if b == "" || a == "" {
		return true
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// pickGooseUnseen picks the newest unseen session by the timestamp embedded
// in the ID; sessions without a parsable timestamp fall back to list order.
func pickGooseUnseen(ids []string, seen map[string]bool) (string, bool) {
	var best string
	var bestT time.Time
	var have bool
	for _, id := range ids {
		if seen[id] {
			continue
		}
		t, ok := gooseStartedAt(id)
		if !ok {
			if best == "" {
				best = id
			}
			continue
		}
		if !have || t.After(bestT) {
			best, bestT, have = id, t, true
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}

func gooseStartedAt(id string) (time.Time, bool) {
	id = strings.TrimSuffix(id, ".jsonl")
	if len(id) < 15 {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("20060102_150405", id[:15], time.Local)
	return t, err == nil
}

// gooseLiveWindow bounds how recently a session must have been updated to be
// offered as an attach target. Goose keeps no per-session process lease, so
// recency plus workspace match is the honest liveness heuristic.
const gooseLiveWindow = 30 * time.Minute

// LiveGoosePicks lists recent Goose sessions (updated within gooseLiveWindow)
// whose working directory matches the current one.
func LiveGoosePicks(ctx context.Context) ([]SessionPick, error) {
	return liveGoosePicks(ctx)
}

var liveGoosePicksFn = liveGoosePicksOS

func liveGoosePicks(ctx context.Context) ([]SessionPick, error) {
	return liveGoosePicksFn(ctx)
}

func liveGoosePicksOS(ctx context.Context) ([]SessionPick, error) {
	out, err := defaultExec(ctx, "goose", []string{"session", "list", "--format", "json", "--limit", "10"})
	if err != nil {
		// No goose binary or daemon-style failure — no live picks, not an error.
		return nil, nil
	}
	return parseGooseSessionPicks(out), nil
}

// parseGooseSessionPicks converts session-list rows into attachable picks,
// keeping only sessions updated within gooseLiveWindow.
func parseGooseSessionPicks(out string) []SessionPick {
	var root struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &root); err != nil || root.Sessions == nil {
		return nil
	}
	cwd, _ := filepath.Abs(".")
	cutoff := time.Now().Add(-gooseLiveWindow)
	var picks []SessionPick
	for _, m := range root.Sessions {
		id := firstString(m, "id")
		if id == "" {
			continue
		}
		ws := firstString(m, "working_dir", "workingDir")
		if ws != "" && cwd != "" && filepath.Clean(ws) != cwd {
			continue
		}
		mod := gooseUpdatedAt(m)
		if !mod.IsZero() && mod.Before(cutoff) {
			continue
		}
		label := firstString(m, "name")
		if label == "" {
			label = shortSessionLabel(id)
		}
		picks = append(picks, SessionPick{
			Agent: "goose", SessionID: id, Workspace: ws, Label: label, Preview: "goose", ModTime: mod,
		})
	}
	return picks
}

func gooseUpdatedAt(m map[string]any) time.Time {
	if s := firstString(m, "updated_at", "updatedAt"); s != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(layout, s); err == nil {
				return t
			}
		}
	}
	if n, ok := asFloat(m["updated_at"]); ok && n > 0 {
		return unixMaybeMs(n)
	}
	return time.Time{}
}
