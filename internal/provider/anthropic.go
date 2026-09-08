package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Anthropic struct {
	id  string
	url string
	key string
}

func NewAnthropic(id, url, key string) *Anthropic {
	if url == "" {
		url = "https://api.anthropic.com"
	}
	return &Anthropic{id: id, url: strings.TrimRight(url, "/"), key: key}
}

func (p *Anthropic) ID() string { return p.id }

func (p *Anthropic) Models(_ context.Context) ([]Model, error) {
	return []Model{{ID: "claude-sonnet-4-5"}, {ID: "claude-haiku-4-5"}}, nil
}

func (p *Anthropic) Capabilities(_ context.Context, _ Model) (Capabilities, error) {
	return Capabilities{Tools: true, Streaming: false}, nil
}

func (p *Anthropic) Generate(ctx context.Context, req Request) (<-chan Event, error) {
	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		if err := p.complete(ctx, req, ch); err != nil {
			ch <- Event{Type: "error", Data: err.Error()}
		}
	}()
	return ch, nil
}

func (p *Anthropic) complete(ctx context.Context, req Request, ch chan<- Event) error {
	system, msgs := anthropicMessages(req.Messages)
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": firstPos(req.MaxTokens, 4096),
		"messages":   msgs,
	}
	if system != "" {
		body["system"] = system
	}
	if len(req.Tools) > 0 {
		body["tools"] = anthropicTools(req.Tools)
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url+"/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.key)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	resp, err := StreamClient().Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var decoded struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		msg := fmt.Sprintf("anthropic: %d", resp.StatusCode)
		if decoded.Error != nil {
			msg += " " + decoded.Error.Message
		}
		return fmt.Errorf("%s", msg)
	}
	var content strings.Builder
	var calls []ToolCall
	for _, c := range decoded.Content {
		switch c.Type {
		case "text":
			content.WriteString(c.Text)
			ch <- Event{Type: "token", Data: c.Text}
		case "tool_use":
			tc := ToolCall{ID: c.ID, Name: c.Name, Arguments: string(c.Input)}
			calls = append(calls, tc)
			ch <- Event{Type: "tool_call", Data: tc}
		}
	}
	usage := Usage{PromptTokens: decoded.Usage.InputTokens, CompletionTokens: decoded.Usage.OutputTokens}
	ch <- Event{Type: "usage", Data: usage}
	ch <- Event{Type: "done", Data: Result{Content: content.String(), ToolCalls: calls, Usage: usage}}
	return nil
}

func anthropicMessages(msgs []Message) (string, []map[string]any) {
	var system string
	var out []map[string]any
	for _, m := range msgs {
		if m.Role == "system" {
			system += m.Content
			continue
		}
		role := m.Role
		if role == "tool" {
			out = append(out, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     m.Content,
				}},
			})
			continue
		}
		if len(m.ToolCalls) > 0 {
			var blocks []map[string]any
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var input any
				if json.Unmarshal([]byte(tc.Arguments), &input) != nil {
					input = map[string]any{}
				}
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": input,
				})
			}
			out = append(out, map[string]any{"role": "assistant", "content": blocks})
			continue
		}
		out = append(out, map[string]any{"role": role, "content": m.Content})
	}
	return system, out
}

func anthropicTools(tools []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if len(params) == 0 {
			params = JSONSchemaObject()
		}
		var schema map[string]any
		_ = json.Unmarshal(params, &schema)
		out = append(out, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": schema,
		})
	}
	return out
}

func firstPos(n, def int) int {
	if n > 0 {
		return n
	}
	return def
}
