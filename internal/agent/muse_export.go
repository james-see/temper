package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type museCursor struct {
	seq int64
}

type museSessionRow struct {
	ID      string
	LogPath string
	ModTime time.Time
}

// parseMuseJSONL maps Muse session.jsonl lines into Temper ingests.
// Each line is an envelope: {sequence, recorded_at, record_type, payload_type, payload}.
func parseMuseJSONL(out string, cur museCursor) ([]Ingest, museCursor) {
	lines := splitNonEmpty(out)
	if len(lines) == 0 {
		return nil, cur
	}
	var evs []Ingest
	maxSeq := cur.seq
	for _, line := range lines {
		env, ok := parseMuseEnvelope(line)
		if !ok {
			continue
		}
		if env.seq <= cur.seq {
			continue
		}
		if env.seq > maxSeq {
			maxSeq = env.seq
		}
		evs = append(evs, ingestMuseEnvelope(env)...)
	}
	cur.seq = maxSeq
	return evs, cur
}

// parseMuseExport maps a muse export JSON document into Temper ingests.
func parseMuseExport(out string, cur museCursor) ([]Ingest, museCursor) {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, cur
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(out), &root); err != nil {
		return parseMuseJSONL(out, cur)
	}
	events, _ := root["events"].([]any)
	if events == nil {
		return nil, cur
	}
	var evs []Ingest
	maxSeq := cur.seq
	for _, item := range events {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		env, ok := museEnvFromExportEvent(m)
		if !ok {
			continue
		}
		if env.seq <= cur.seq {
			continue
		}
		if env.seq > maxSeq {
			maxSeq = env.seq
		}
		evs = append(evs, ingestMuseEnvelope(env)...)
	}
	cur.seq = maxSeq
	return evs, cur
}

type museEnv struct {
	seq         int64
	payloadType string
	eventKind   string
	payload     map[string]any
}

func parseMuseEnvelope(line string) (museEnv, bool) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return museEnv{}, false
	}
	seq := museSeq(raw["sequence"])
	payloadType := firstString(raw, "payload_type")
	payload, _ := raw["payload"].(map[string]any)
	eventKind := ""
	if payload != nil {
		if ev, ok := payload["event"].(map[string]any); ok {
			eventKind = firstString(ev, "kind")
		}
		if eventKind == "" {
			eventKind = firstString(payload, "kind", "event_kind")
		}
	}
	if payloadType == "" {
		payloadType = firstString(raw, "record_type")
	}
	return museEnv{seq: seq, payloadType: payloadType, eventKind: eventKind, payload: payload}, true
}

func museEnvFromExportEvent(m map[string]any) (museEnv, bool) {
	// Export events may nest envelope under "envelope" or be flat.
	if env, ok := m["envelope"].(map[string]any); ok {
		seq := museSeq(env["sequence"])
		payload, _ := env["payload"].(map[string]any)
		payloadType := firstString(env, "payload_type")
		eventKind := ""
		if payload != nil {
			if ev, ok := payload["event"].(map[string]any); ok {
				eventKind = firstString(ev, "kind")
			}
		}
		if eventKind == "" {
			eventKind = firstString(m, "kind")
		}
		return museEnv{seq: seq, payloadType: payloadType, eventKind: eventKind, payload: payload}, true
	}
	return parseMuseEnvelope(mustJSON(m))
}

func mustJSON(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}

func museSeq(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	default:
		return 0
	}
}

func ingestMuseEnvelope(env museEnv) []Ingest {
	kind := strings.ToLower(env.eventKind)
	pt := strings.ToLower(env.payloadType)
	key := kind
	if key == "" {
		key = pt
	}
	switch {
	case museIsUserPrompt(key, env.payload):
		if text := museUserText(env.payload); text != "" {
			return []Ingest{{Type: "user.message", Data: map[string]any{"text": text}}}
		}
	case strings.Contains(key, "approval.review") || strings.HasPrefix(key, "approval_wait"):
		msg := "approval pending"
		if text := museToolArgs(env.payload); text != "" {
			msg = "approval pending: " + clip(text, 200)
		}
		return []Ingest{{Type: "user.message", Data: map[string]any{"text": msg, "kind": "approval"}}}
	case strings.Contains(key, "side_effect_intent") || strings.HasSuffix(key, "tool_batch.effect.started") ||
		strings.Contains(key, "tool_batch.effect.started"):
		name, args := museToolCall(env.payload)
		if name == "" {
			name = "muse"
		}
		return []Ingest{{Type: "tool.requested", Data: map[string]any{"tool": name, "args": args}}}
	case strings.Contains(key, "tool_batch.effect.terminal") || strings.Contains(key, "tool.effect.terminal") ||
		strings.Contains(key, "edit") && strings.Contains(key, "terminal"):
		name, out := museToolResult(env.payload)
		if name == "" {
			name = "muse"
		}
		data := map[string]any{"tool": name, "output": out}
		if err := museToolErr(env.payload, out); err != "" {
			data["error"] = err
		}
		return []Ingest{{Type: "tool.completed", Data: data}}
	case strings.Contains(key, "model") || strings.Contains(key, "llm") || strings.Contains(key, "assistant"):
		var out []Ingest
		if reason := museReasoning(env.payload); reason != "" {
			out = append(out, thinkingIngest(len(reason), len(reason), nil, false, reason))
		}
		if text := museAssistantText(env.payload); text != "" {
			out = append(out, Ingest{Type: "model.completed", Data: map[string]any{"content": text}})
		}
		return out
	case strings.Contains(key, "message") || strings.Contains(key, "turn.completed"):
		if text := museAssistantText(env.payload); text != "" {
			return []Ingest{{Type: "model.completed", Data: map[string]any{"content": text}}}
		}
	}
	return nil
}

func museIsUserPrompt(key string, payload map[string]any) bool {
	if strings.Contains(key, "user") || strings.Contains(key, "prompt.submit") ||
		strings.Contains(key, "user_prompt") || strings.Contains(key, "user.message") {
		return true
	}
	if payload == nil {
		return false
	}
	role := strings.ToLower(firstString(payload, "role", "speaker"))
	return role == "user" || role == "human"
}

func museUserText(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if s := firstString(payload, "text", "prompt", "content", "message", "query"); s != "" {
		return s
	}
	if ev, ok := payload["event"].(map[string]any); ok {
		if s := firstString(ev, "text", "prompt", "content"); s != "" {
			return s
		}
	}
	if msg, ok := payload["message"].(map[string]any); ok {
		return messageText(msg)
	}
	return ""
}

func museAssistantText(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if s := firstString(payload, "text", "content", "response", "output"); s != "" {
		return s
	}
	if ev, ok := payload["event"].(map[string]any); ok {
		if s := firstString(ev, "text", "content", "response"); s != "" {
			return s
		}
	}
	return ""
}

func museReasoning(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if s := firstString(payload, "reasoning", "thinking", "reasoning_content"); s != "" {
		return s
	}
	if ev, ok := payload["event"].(map[string]any); ok {
		return firstString(ev, "reasoning", "thinking")
	}
	return ""
}

func museToolCall(payload map[string]any) (name, args string) {
	if payload == nil {
		return "", ""
	}
	name = firstString(payload, "tool", "tool_name", "name", "operation")
	args = firstString(payload, "args", "arguments", "input", "command")
	if strings.HasPrefix(name, "tool:") {
		name = strings.TrimPrefix(name, "tool:")
	}
	if op := firstString(payload, "operation"); strings.HasPrefix(op, "tool:") {
		name = strings.TrimPrefix(op, "tool:")
	}
	if ev, ok := payload["event"].(map[string]any); ok {
		if n := firstString(ev, "tool", "tool_name", "name"); n != "" {
			name = n
		}
		if a := firstString(ev, "args", "arguments", "input", "command"); a != "" {
			args = a
		}
	}
	return name, args
}

func museToolArgs(payload map[string]any) string {
	_, args := museToolCall(payload)
	return args
}

func museToolResult(payload map[string]any) (name, output string) {
	name, _ = museToolCall(payload)
	if payload == nil {
		return name, ""
	}
	output = firstString(payload, "output", "result", "stdout", "content", "text")
	if output == "" {
		if ev, ok := payload["event"].(map[string]any); ok {
			output = firstString(ev, "output", "result", "stdout", "content")
		}
	}
	return name, output
}

func museToolErr(payload map[string]any, output string) string {
	if payload == nil {
		return ""
	}
	if e := firstString(payload, "error", "err"); e != "" && e != "null" {
		return e
	}
	if ev, ok := payload["event"].(map[string]any); ok {
		if e := firstString(ev, "error"); e != "" {
			return e
		}
	}
	if strings.Contains(strings.ToLower(output), "error") {
		return clip(output, 200)
	}
	return ""
}

func pickMuseUnseen(ids []string, seen map[string]bool) (string, bool) {
	// listMuseSessionIDs returns newest-first.
	for _, id := range ids {
		if !seen[id] {
			return id, true
		}
	}
	return "", false
}

func listMuseSessionIDs(root string) ([]string, error) {
	rows, err := scanMuseSessions(root)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

func scanMuseSessions(root string) ([]museSessionRow, error) {
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
	var rows []museSessionRow
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != "session.jsonl" {
			return nil
		}
		id := filepath.Base(filepath.Dir(path))
		if id == "" || id == "." {
			return nil
		}
		mod := time.Time{}
		if fi, e := d.Info(); e == nil {
			mod = fi.ModTime()
		}
		rows = append(rows, museSessionRow{ID: id, LogPath: path, ModTime: mod})
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

func findMuseSessionLog(root, id string) string {
	id = strings.TrimSpace(id)
	if id == "" || root == "" {
		return ""
	}
	rows, err := scanMuseSessions(root)
	if err != nil {
		return ""
	}
	for _, r := range rows {
		if r.ID == id || strings.HasPrefix(r.ID, id) || strings.HasPrefix(id, r.ID) {
			return r.LogPath
		}
	}
	// Direct glob: .../YYYY/MM/DD/<id>/session.jsonl
	matches, _ := filepath.Glob(filepath.Join(root, "*", "*", "*", id, "session.jsonl"))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}
