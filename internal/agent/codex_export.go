package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type codexCursor struct {
	n int // lines consumed
}

type codexSessionRow struct {
	ID      string
	LogPath string
	ModTime time.Time
	CWD     string
}

const codexLiveWindow = 30 * time.Minute

// parseCodexJSONL maps Codex session rollouts / exec --json events into Temper ingests.
// Capture a real fixture later with:
//
//	CODEX_HOME=/tmp/cx codex exec --json "hi"
//
// then copy $CODEX_HOME/sessions/**/*.jsonl.
func parseCodexJSONL(out string, cur codexCursor) ([]Ingest, codexCursor) {
	lines := splitNonEmpty(out)
	if len(lines) == 0 {
		return nil, cur
	}
	if cur.n > len(lines) {
		cur.n = 0
	}
	var evs []Ingest
	for _, line := range lines[cur.n:] {
		evs = append(evs, ingestCodexLine(line)...)
	}
	cur.n = len(lines)
	return evs, cur
}

func ingestCodexLine(line string) []Ingest {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil
	}
	return ingestCodexRecord(raw)
}

func ingestCodexRecord(raw map[string]any) []Ingest {
	typ := strings.ToLower(firstString(raw, "type", "event", "kind"))

	// Exec --json lifecycle events.
	switch {
	case typ == "thread.started" || typ == "turn.started" || typ == "turn.completed" ||
		typ == "error" || typ == "turn.failed":
		if typ == "error" || typ == "turn.failed" {
			msg := firstString(raw, "message", "error", "reason")
			if msg == "" {
				msg = typ
			}
			return []Ingest{{Type: "tool.completed", Data: map[string]any{"tool": "codex", "error": clip(msg, 400), "output": msg}}}
		}
		return nil
	case strings.HasPrefix(typ, "item."):
		return ingestCodexItem(typ, raw)
	}

	// Conversation / rollout style records.
	if msg, ok := raw["message"].(map[string]any); ok {
		if nested := ingestCodexMessage(msg); len(nested) > 0 {
			return nested
		}
	}
	if payload, ok := raw["payload"].(map[string]any); ok {
		if nested := ingestCodexRecord(payload); len(nested) > 0 {
			return nested
		}
	}
	return ingestCodexMessage(raw)
}

func ingestCodexItem(typ string, raw map[string]any) []Ingest {
	item, _ := raw["item"].(map[string]any)
	if item == nil {
		item = raw
	}
	itemType := strings.ToLower(firstString(item, "type", "kind", "item_type"))
	name := firstString(item, "command", "name", "tool", "path")
	if name == "" {
		name = "codex"
	}
	args := firstString(item, "command", "arguments", "args", "input", "cwd")
	if args == "" {
		args = codexArgsString(item)
	}
	if itemType == "agent_message" || itemType == "message" || itemType == "assistant" {
		text := firstString(item, "text", "content", "message")
		if text != "" {
			return []Ingest{{Type: "model.completed", Data: map[string]any{"content": text}}}
		}
		return nil
	}
	if itemType == "reasoning" || itemType == "thinking" {
		text := firstString(item, "text", "content", "thinking")
		if text != "" {
			return []Ingest{thinkingIngest(len(text), len(text), nil, false, text)}
		}
		return nil
	}
	switch {
	case strings.Contains(typ, "started") || strings.HasSuffix(typ, ".started"):
		return []Ingest{{Type: "tool.requested", Data: map[string]any{"tool": nzTool(name, itemType), "args": args}}}
	case strings.Contains(typ, "completed") || strings.Contains(typ, "failed") || strings.HasSuffix(typ, ".updated"):
		out := firstString(item, "output", "result", "stdout", "content", "text")
		if out == "" {
			out = codexArgsString(item)
		}
		data := map[string]any{"tool": nzTool(name, itemType), "output": out}
		if strings.Contains(typ, "failed") || firstString(item, "status") == "failed" {
			data["error"] = clip(out, 400)
		}
		return []Ingest{{Type: "tool.completed", Data: data}}
	}
	return nil
}

func nzTool(name, fallback string) string {
	if name != "" && name != "{}" {
		return name
	}
	if fallback != "" {
		return fallback
	}
	return "codex"
}

func ingestCodexMessage(raw map[string]any) []Ingest {
	typ := strings.ToLower(firstString(raw, "type", "role"))
	switch typ {
	case "system", "meta", "session_meta", "session_start", "":
		if role := strings.ToLower(firstString(raw, "role")); role == "user" || role == "assistant" {
			raw["type"] = role
			return ingestCodexMessage(raw)
		}
		return nil
	case "user", "human":
		if results := codexToolResults(raw); len(results) > 0 {
			return results
		}
		text := codexTextContent(raw)
		if text == "" {
			text = firstString(raw, "text", "content", "prompt")
		}
		if text != "" {
			return []Ingest{{Type: "user.message", Data: map[string]any{"text": text}}}
		}
		return nil
	case "assistant", "ai", "model", "agent":
		var out []Ingest
		if reason := codexThinkingContent(raw); reason != "" {
			out = append(out, thinkingIngest(len(reason), len(reason), nil, false, reason))
		}
		for _, tc := range codexToolUses(raw) {
			out = append(out, Ingest{Type: "tool.requested", Data: map[string]any{"tool": tc.name, "args": tc.args}})
		}
		if text := codexTextContent(raw); text != "" {
			out = append(out, Ingest{Type: "model.completed", Data: map[string]any{"content": text, "tool_calls": len(codexToolUses(raw))}})
		}
		return out
	case "function_call", "tool_call", "tool_use":
		name := firstString(raw, "name", "tool")
		args := firstString(raw, "arguments", "args", "input")
		if args == "" {
			args = codexArgsString(raw["arguments"])
		}
		if name == "" {
			name = "codex"
		}
		return []Ingest{{Type: "tool.requested", Data: map[string]any{"tool": name, "args": args}}}
	case "function_call_output", "tool_result", "tool_output":
		name := firstString(raw, "name", "tool", "call_id")
		if name == "" {
			name = "codex"
		}
		out := firstString(raw, "output", "content", "result")
		data := map[string]any{"tool": name, "output": out}
		return []Ingest{{Type: "tool.completed", Data: data}}
	default:
		return nil
	}
}

func codexContentBlocks(raw map[string]any) []any {
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

func codexTextContent(raw map[string]any) string {
	if s := firstString(raw, "text"); s != "" {
		return s
	}
	if s, ok := raw["content"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	for _, item := range codexContentBlocks(raw) {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(firstString(block, "type"))
		if kind != "text" && kind != "output_text" && kind != "" {
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

func codexThinkingContent(raw map[string]any) string {
	if s := firstString(raw, "thinking", "reasoning"); s != "" {
		return s
	}
	var b strings.Builder
	for _, item := range codexContentBlocks(raw) {
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

func codexToolUses(raw map[string]any) []toolCall {
	var out []toolCall
	for _, item := range codexContentBlocks(raw) {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(firstString(block, "type"))
		if kind != "tool_use" && kind != "function_call" && kind != "tool_call" {
			continue
		}
		name := firstString(block, "name", "tool")
		args := firstString(block, "input", "arguments", "args")
		if args == "" {
			args = codexArgsString(block["input"])
			if args == "" {
				args = codexArgsString(block["arguments"])
			}
		}
		if name == "" {
			continue
		}
		out = append(out, toolCall{name: name, args: args})
	}
	return out
}

func codexToolResults(raw map[string]any) []Ingest {
	var out []Ingest
	for _, item := range codexContentBlocks(raw) {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.ToLower(firstString(block, "type"))
		if kind != "tool_result" && kind != "function_call_output" {
			continue
		}
		name := firstString(block, "tool_use_id", "call_id", "name", "tool")
		if name == "" {
			name = "codex"
		}
		content := firstString(block, "content", "output", "text")
		if content == "" {
			content = codexArgsString(block["content"])
		}
		data := map[string]any{"tool": name, "output": content}
		out = append(out, Ingest{Type: "tool.completed", Data: data})
	}
	return out
}

func codexArgsString(v any) string {
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

func pickCodexUnseen(ids []string, seen map[string]bool) (string, bool) {
	for _, id := range ids {
		if !seen[id] {
			return id, true
		}
	}
	return "", false
}

func listCodexSessionIDs(root string) ([]string, error) {
	rows, err := scanCodexSessions(root)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func scanCodexSessions(root string) ([]codexSessionRow, error) {
	if root == "" {
		return nil, nil
	}
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}
	var rows []codexSessionRow
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		id := strings.TrimSuffix(d.Name(), ".jsonl")
		// Some rollouts are named rollout-<uuid>.jsonl or similar.
		if strings.HasPrefix(id, "rollout-") {
			id = strings.TrimPrefix(id, "rollout-")
		}
		if id == "" {
			return nil
		}
		mod := time.Time{}
		if fi, e := d.Info(); e == nil {
			mod = fi.ModTime()
		}
		rows = append(rows, codexSessionRow{ID: id, LogPath: path, ModTime: mod})
		return nil
	})
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].ModTime.Equal(rows[j].ModTime) {
			return rows[i].ModTime.After(rows[j].ModTime)
		}
		return rows[i].ID > rows[j].ID
	})
	return rows, nil
}

func findCodexSessionLog(root, id string) string {
	id = strings.TrimSpace(id)
	if id == "" || root == "" {
		return ""
	}
	rows, err := scanCodexSessions(root)
	if err != nil {
		return ""
	}
	for _, r := range rows {
		if r.ID == id || strings.HasPrefix(r.ID, id) || strings.HasPrefix(id, r.ID) ||
			strings.Contains(r.LogPath, id) {
			return r.LogPath
		}
	}
	return ""
}

// LiveCodexPicks lists recent Codex sessions updated within codexLiveWindow.
func LiveCodexPicks(ctx context.Context) ([]SessionPick, error) {
	return liveCodexPicks(ctx)
}

var codexSessionsRootFn = codexSessionsRoot

func liveCodexPicks(ctx context.Context) ([]SessionPick, error) {
	_ = ctx
	root := codexSessionsRootFn()
	rows, err := scanCodexSessions(root)
	if err != nil {
		return nil, nil
	}
	cutoff := time.Now().Add(-codexLiveWindow)
	ws, _ := os.Getwd()
	var picks []SessionPick
	seen := map[string]bool{}
	for _, r := range rows {
		if seen[r.ID] {
			continue
		}
		if !r.ModTime.IsZero() && r.ModTime.Before(cutoff) {
			continue
		}
		seen[r.ID] = true
		picks = append(picks, SessionPick{
			Agent: "codex", SessionID: r.ID, Workspace: ws,
			Label: shortSessionLabel(r.ID), Preview: "codex", ModTime: r.ModTime,
		})
	}
	return picks, nil
}
