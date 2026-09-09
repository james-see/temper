package agent

import (
	"encoding/json"
	"strings"
	"time"
)

func parseExportOutput(out string, seen int) ([]Ingest, int) {
	var raw map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &raw) == nil {
		if msgs, ok := raw["messages"].([]any); ok {
			return parseMessageSlice(msgs, seen), len(msgs)
		}
	}
	lines := splitNonEmpty(out)
	if len(lines) == 0 {
		return nil, seen
	}
	start := seen
	if start > len(lines) {
		start = 0
	}
	var evs []Ingest
	for _, line := range lines[start:] {
		evs = append(evs, parseMessageLine(line)...)
	}
	return evs, len(lines)
}

func parseMessageLine(line string) []Ingest {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil
	}
	if msgs, ok := raw["messages"].([]any); ok {
		return parseMessageSlice(msgs, 0)
	}
	return ingestMessage(raw)
}

func parseMessageSlice(msgs []any, seen int) []Ingest {
	if seen > len(msgs) {
		seen = 0
	}
	var evs []Ingest
	for _, item := range msgs[seen:] {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		evs = append(evs, ingestMessage(raw)...)
	}
	return evs
}

func ingestMessage(raw map[string]any) []Ingest {
	role := strings.ToLower(asString(raw["role"]))
	if role == "" {
		role = strings.ToLower(asString(raw["type"]))
	}
	switch role {
	case "system", "session_meta", "":
		return nil
	case "user", "human":
		if text := messageText(raw); text != "" {
			return []Ingest{{Type: "user.message", Data: map[string]any{"text": text}}}
		}
		return nil
	case "tool":
		name := firstString(raw, "tool_name", "name", "tool")
		if name == "" {
			name = "hermes"
		}
		data := map[string]any{"tool": name, "output": messageText(raw)}
		if err := firstString(raw, "error"); err != "" {
			data["error"] = err
		}
		return []Ingest{{Type: "tool.completed", Data: data}}
	case "assistant", "model", "agent", "ai":
		var out []Ingest
		calls := extractToolCalls(raw)
		for _, tc := range calls {
			out = append(out, Ingest{Type: "tool.requested", Data: map[string]any{"tool": tc.name, "args": tc.args}})
		}
		if text := messageText(raw); text != "" {
			out = append(out, Ingest{Type: "model.completed", Data: map[string]any{"content": text, "tool_calls": len(calls)}})
		}
		return out
	default:
		return nil
	}
}

type toolCall struct {
	name string
	args string
}

func extractToolCalls(raw map[string]any) []toolCall {
	v, ok := raw["tool_calls"]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []toolCall
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := firstString(m, "name", "tool")
		args := firstString(m, "arguments", "args", "input")
		if fn, ok := m["function"].(map[string]any); ok {
			if n := firstString(fn, "name"); n != "" {
				name = n
			}
			if a := firstString(fn, "arguments"); a != "" {
				args = a
			}
		}
		if name == "" {
			continue
		}
		out = append(out, toolCall{name: name, args: args})
	}
	return out
}

func messageText(raw map[string]any) string {
	if s := firstString(raw, "content", "text", "message", "query"); s != "" {
		return s
	}
	if m, ok := raw["message"].(map[string]any); ok {
		return firstString(m, "content", "text")
	}
	for _, key := range []string{"content", "text"} {
		if s := anyText(raw[key]); s != "" {
			return s
		}
	}
	return ""
}

func anyText(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []any:
		var b strings.Builder
		for _, item := range t {
			switch p := item.(type) {
			case string:
				b.WriteString(p)
			case map[string]any:
				if s := firstString(p, "text", "content"); s != "" {
					if b.Len() > 0 {
						b.WriteByte(' ')
					}
					b.WriteString(s)
				}
			}
		}
		return strings.TrimSpace(b.String())
	default:
		return ""
	}
}

func parseExportLine(line string) (Ingest, bool) {
	ings := parseMessageLine(line)
	if len(ings) == 0 {
		return Ingest{}, false
	}
	return ings[0], true
}

func classifyLogLine(line string) (Ingest, bool) {
	low := strings.ToLower(line)
	if strings.Contains(low, "http request") || strings.Contains(low, "plugin ") {
		return Ingest{}, false
	}
	if strings.Contains(low, "traceback") || strings.Contains(low, " exception") ||
		strings.HasPrefix(low, "error") || strings.Contains(low, " error:") {
		return Ingest{Type: "tool.completed", Data: map[string]any{"tool": "hermes", "error": clip(line, 400)}}, true
	}
	return Ingest{}, false
}

func pickUnseen(ids []string, seen map[string]bool) (string, bool) {
	var best string
	var bestT time.Time
	var have bool
	for _, id := range ids {
		if seen[id] {
			continue
		}
		t, ok := sessionStartedAt(id)
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

func sessionStartedAt(id string) (time.Time, bool) {
	m := sessionIDRe.FindStringSubmatch(id)
	if len(m) < 2 {
		return time.Time{}, false
	}
	stamp := m[1]
	if len(stamp) < 15 {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("20060102_150405", stamp[:15], time.Local)
	return t, err == nil
}
