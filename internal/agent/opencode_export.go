package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ocCursor struct {
	n        int
	lastID   string
	thinkLen int
}

// parseOpenCodeExport maps OpenCode export JSON or message arrays into Temper ingests.
// Accepts:
//   - full export: {"info":..., "messages":[{"info":..., "parts":[...]}, ...]}
//   - message list: [{"info":..., "parts":[...]}, ...]
//   - HTTP /session/:id/message response (same as message list)
func parseOpenCodeExport(out string, cur ocCursor) ([]Ingest, ocCursor) {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, cur
	}
	var root any
	if err := json.Unmarshal([]byte(out), &root); err != nil {
		return nil, cur
	}
	msgs := openCodeMessages(root)
	if msgs == nil {
		return nil, cur
	}
	return parseOpenCodeMessages(msgs, cur)
}

func openCodeMessages(root any) []any {
	switch v := root.(type) {
	case []any:
		return v
	case map[string]any:
		if msgs, ok := v["messages"].([]any); ok {
			return msgs
		}
		if data, ok := v["data"].(map[string]any); ok {
			if msgs, ok := data["messages"].([]any); ok {
				return msgs
			}
		}
		// Single message object.
		if _, hasParts := v["parts"]; hasParts {
			return []any{v}
		}
		if info, ok := v["info"].(map[string]any); ok {
			if _, hasRole := info["role"]; hasRole {
				return []any{v}
			}
		}
	}
	return nil
}

func parseOpenCodeMessages(msgs []any, cur ocCursor) ([]Ingest, ocCursor) {
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
		evs = append(evs, ingestOpenCodeMessage(raw)...)
	}
	if !grew {
		if ing, ok := openCodeThinkGrowth(msgs, cur); ok {
			evs = append(evs, ing)
		}
	}
	cur.n = len(msgs)
	if last, ok := lastMessage(msgs); ok {
		cur.lastID = openCodeMessageID(last)
		cur.thinkLen = len(openCodeReasoning(last))
	}
	return evs, cur
}

func openCodeThinkGrowth(msgs []any, cur ocCursor) (Ingest, bool) {
	last, ok := lastMessage(msgs)
	if !ok {
		return Ingest{}, false
	}
	id := openCodeMessageID(last)
	rlen := len(openCodeReasoning(last))
	if id == "" || id != cur.lastID || rlen <= cur.thinkLen {
		return Ingest{}, false
	}
	text := openCodeText(last)
	calls := openCodeToolCalls(last)
	return thinkingIngest(rlen, rlen-cur.thinkLen, calls, text != "", openCodeReasoning(last)), true
}

func openCodeMessageID(raw map[string]any) string {
	if s := firstString(raw, "id"); s != "" {
		return s
	}
	if info, ok := raw["info"].(map[string]any); ok {
		if s := firstString(info, "id"); s != "" {
			return s
		}
		if info["id"] != nil {
			return fmt.Sprint(info["id"])
		}
	}
	if raw["id"] != nil {
		return fmt.Sprint(raw["id"])
	}
	return ""
}

func ingestOpenCodeMessage(raw map[string]any) []Ingest {
	info := raw
	if nested, ok := raw["info"].(map[string]any); ok {
		info = nested
	}
	role := strings.ToLower(firstString(info, "role", "type"))
	switch role {
	case "system", "session_meta", "":
		// Still check parts for tool results attached without a clear role.
		if role == "" && openCodeHasToolParts(raw) {
			return ingestOpenCodeParts(raw, "assistant")
		}
		return nil
	case "user", "human":
		if text := openCodeText(raw); text != "" {
			return []Ingest{{Type: "user.message", Data: map[string]any{"text": text}}}
		}
		return nil
	case "tool":
		name := firstString(info, "tool_name", "name", "tool")
		if name == "" {
			name = "opencode"
		}
		text := openCodeText(raw)
		data := map[string]any{"tool": name, "output": text}
		if err := toolError(info, text); err != "" {
			data["error"] = err
		}
		return []Ingest{{Type: "tool.completed", Data: data}}
	case "assistant", "model", "agent", "ai":
		return ingestOpenCodeParts(raw, role)
	default:
		return ingestOpenCodeParts(raw, role)
	}
}

func openCodeHasToolParts(raw map[string]any) bool {
	parts, ok := raw["parts"].([]any)
	if !ok {
		return false
	}
	for _, p := range parts {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(firstString(m, "type"))
		if typ == "tool" || typ == "tool-result" || typ == "tool_result" || typ == "tool-invocation" {
			return true
		}
	}
	return false
}

func ingestOpenCodeParts(raw map[string]any, role string) []Ingest {
	var out []Ingest
	calls := openCodeToolCalls(raw)
	text := openCodeText(raw)
	if reason := openCodeReasoning(raw); reason != "" {
		out = append(out, thinkingIngest(len(reason), len(reason), calls, text != "", reason))
	}
	for _, tc := range calls {
		out = append(out, Ingest{Type: "tool.requested", Data: map[string]any{"tool": tc.name, "args": tc.args}})
	}
	out = append(out, openCodeToolResults(raw)...)
	if text != "" && (role == "assistant" || role == "model" || role == "agent" || role == "ai") {
		out = append(out, Ingest{Type: "model.completed", Data: map[string]any{"content": text, "tool_calls": len(calls)}})
	}
	return out
}

func openCodeToolCalls(raw map[string]any) []toolCall {
	var out []toolCall
	out = append(out, extractToolCalls(raw)...)
	if info, ok := raw["info"].(map[string]any); ok {
		out = append(out, extractToolCalls(info)...)
	}
	parts, ok := raw["parts"].([]any)
	if !ok {
		return out
	}
	for _, p := range parts {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(firstString(m, "type"))
		switch typ {
		case "tool", "tool-invocation", "tool_use", "tool-call", "tool_call":
			name := firstString(m, "tool", "name", "toolName", "tool_name")
			if name == "" {
				if state, ok := m["state"].(map[string]any); ok {
					name = firstString(state, "tool", "name")
				}
			}
			if name == "" {
				continue
			}
			args := firstString(m, "arguments", "args", "input")
			if args == "" {
				if input, ok := m["input"]; ok {
					args = stringifyJSON(input)
				} else if state, ok := m["state"].(map[string]any); ok {
					if input, ok := state["input"]; ok {
						args = stringifyJSON(input)
					}
				}
			}
			out = append(out, toolCall{name: name, args: args})
		}
	}
	return out
}

func openCodeToolResults(raw map[string]any) []Ingest {
	parts, ok := raw["parts"].([]any)
	if !ok {
		return nil
	}
	var out []Ingest
	for _, p := range parts {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(firstString(m, "type"))
		if typ != "tool-result" && typ != "tool_result" && typ != "tool-response" {
			// Completed tool part with output in state.
			if typ == "tool" {
				state, ok := m["state"].(map[string]any)
				if !ok {
					continue
				}
				status := strings.ToLower(firstString(state, "status"))
				if status != "completed" && status != "error" && state["output"] == nil && state["error"] == nil {
					continue
				}
				name := firstString(m, "tool", "name", "toolName")
				if name == "" {
					name = firstString(state, "tool", "name")
				}
				if name == "" {
					name = "opencode"
				}
				output := firstString(state, "output", "text", "content")
				if output == "" {
					output = stringifyJSON(state["output"])
				}
				data := map[string]any{"tool": name, "output": output}
				if err := firstString(state, "error"); err != "" {
					data["error"] = err
				} else if status == "error" {
					data["error"] = "tool error"
				}
				out = append(out, Ingest{Type: "tool.completed", Data: data})
			}
			continue
		}
		name := firstString(m, "tool", "name", "toolName", "tool_name")
		if name == "" {
			name = "opencode"
		}
		output := firstString(m, "output", "text", "content", "result")
		if output == "" {
			output = stringifyJSON(m["output"])
		}
		data := map[string]any{"tool": name, "output": output}
		if err := firstString(m, "error"); err != "" {
			data["error"] = err
		}
		out = append(out, Ingest{Type: "tool.completed", Data: data})
	}
	return out
}

func openCodeReasoning(raw map[string]any) string {
	if s := firstString(raw, "reasoning", "reasoning_content"); s != "" {
		return s
	}
	if info, ok := raw["info"].(map[string]any); ok {
		if s := firstString(info, "reasoning", "reasoning_content"); s != "" {
			return s
		}
	}
	parts, ok := raw["parts"].([]any)
	if !ok {
		return anyReasoning(raw["content"])
	}
	var b strings.Builder
	for _, p := range parts {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(firstString(m, "type"))
		if typ != "thinking" && typ != "reasoning" && typ != "think" && typ != "reasoning-delta" {
			continue
		}
		if s := firstString(m, "thinking", "reasoning", "text", "content"); s != "" {
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(s)
		}
	}
	if b.Len() > 0 {
		return strings.TrimSpace(b.String())
	}
	return anyReasoning(raw["content"])
}

func openCodeText(raw map[string]any) string {
	if s := firstString(raw, "content", "text", "message", "query"); s != "" {
		// Prefer parts when present; flat content may be a summary.
		if _, ok := raw["parts"]; !ok {
			return s
		}
	}
	parts, ok := raw["parts"].([]any)
	if ok {
		var b strings.Builder
		for _, p := range parts {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			typ := strings.ToLower(firstString(m, "type"))
			if typ != "" && typ != "text" && typ != "content" {
				continue
			}
			if s := firstString(m, "text", "content"); s != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(s)
			}
		}
		if t := strings.TrimSpace(b.String()); t != "" {
			return t
		}
	}
	if info, ok := raw["info"].(map[string]any); ok {
		if s := firstString(info, "content", "text"); s != "" {
			return s
		}
	}
	return messageText(raw)
}

func stringifyJSON(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func parseOpenCodeSessionIDs(out string) []string {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil
	}
	var root any
	if err := json.Unmarshal([]byte(out), &root); err != nil {
		return parseOpenCodeSessionIDsTable(out)
	}
	var ids []string
	seen := map[string]bool{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	switch v := root.(type) {
	case []any:
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if s := firstString(m, "id", "sessionID", "session_id"); s != "" {
				add(s)
			}
		}
	case map[string]any:
		if arr, ok := v["sessions"].([]any); ok {
			for _, item := range arr {
				m, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if s := firstString(m, "id", "sessionID", "session_id"); s != "" {
					add(s)
				}
			}
		} else if s := firstString(v, "id"); s != "" {
			add(s)
		}
	}
	return ids
}

func parseOpenCodeSessionIDsTable(out string) []string {
	var ids []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		for _, f := range fields {
			if strings.HasPrefix(f, "ses_") || strings.HasPrefix(f, "session_") {
				if !seen[f] {
					seen[f] = true
					ids = append(ids, f)
				}
			}
		}
	}
	return ids
}

func pickOpenCodeUnseen(ids []string, seen map[string]bool) (string, bool) {
	// Session list is newest-first from OpenCode; pick first unseen.
	for _, id := range ids {
		if !seen[id] {
			return id, true
		}
	}
	return "", false
}
