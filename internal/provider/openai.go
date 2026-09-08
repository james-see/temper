package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type OpenAI struct {
	id  string
	url string
	key string
}

func NewOpenAI(id, url, key string) *OpenAI {
	if url == "" {
		url = "https://api.openai.com/v1"
	}
	return &OpenAI{id: id, url: strings.TrimRight(url, "/"), key: key}
}

func NewOMLX(id, url, key string) *OpenAI {
	if url == "" {
		url = "http://localhost:8000"
	}
	if !strings.HasSuffix(url, "/v1") && !strings.Contains(url, "/v1/") {
		url = strings.TrimRight(url, "/") + "/v1"
	}
	return NewOpenAI(id, url, key)
}

func (p *OpenAI) ID() string { return p.id }

func (p *OpenAI) Models(ctx context.Context) ([]Model, error) {
	b, code, err := DoJSON(ctx, http.MethodGet, p.url+"/models", p.key, "", nil, nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("openai models: %d %s", code, b)
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(resp.Data))
	for _, d := range resp.Data {
		out = append(out, Model{ID: d.ID})
	}
	return out, nil
}

func (p *OpenAI) Capabilities(_ context.Context, _ Model) (Capabilities, error) {
	return Capabilities{Tools: true, Streaming: true, Local: strings.Contains(p.url, "localhost")}, nil
}

func (p *OpenAI) Generate(ctx context.Context, req Request) (<-chan Event, error) {
	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		if err := p.stream(ctx, req, ch); err != nil {
			ch <- Event{Type: "error", Data: err.Error()}
		}
	}()
	return ch, nil
}

func (p *OpenAI) stream(ctx context.Context, req Request, ch chan<- Event) error {
	body := map[string]any{
		"model":    req.Model,
		"messages": openaiMessages(req.Messages),
		"stream":   true,
		"stream_options": map[string]any{
			"include_usage": true,
		},
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		body["tools"] = openaiTools(req.Tools)
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.key)
	}
	resp, err := StreamClient().Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("openai: %d %s", resp.StatusCode, b)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var content strings.Builder
	toolArgs := map[int]*ToolCall{}
	var usage Usage
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			usage = Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.Content != "" {
			content.WriteString(d.Content)
			ch <- Event{Type: "token", Data: d.Content}
		}
		for _, tc := range d.ToolCalls {
			cur, ok := toolArgs[tc.Index]
			if !ok {
				cur = &ToolCall{ID: tc.ID, Name: tc.Function.Name}
				toolArgs[tc.Index] = cur
			}
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			if tc.Function.Name != "" {
				cur.Name = tc.Function.Name
			}
			cur.Arguments += tc.Function.Arguments
		}
	}
	var calls []ToolCall
	for i := 0; i < len(toolArgs); i++ {
		if tc, ok := toolArgs[i]; ok {
			calls = append(calls, *tc)
			ch <- Event{Type: "tool_call", Data: *tc}
		}
	}
	if usage.PromptTokens+usage.CompletionTokens > 0 {
		ch <- Event{Type: "usage", Data: usage}
	}
	ch <- Event{Type: "done", Data: Result{Content: content.String(), ToolCalls: calls, Usage: usage}}
	return sc.Err()
}

func openaiMessages(msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		row := map[string]any{"role": m.Role, "content": m.Content}
		if m.Name != "" {
			row["name"] = m.Name
		}
		if m.ToolCallID != "" {
			row["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			var tcs []map[string]any
			for _, tc := range m.ToolCalls {
				tcs = append(tcs, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": tc.Arguments,
					},
				})
			}
			row["tool_calls"] = tcs
		}
		out = append(out, row)
	}
	return out
}

func openaiTools(tools []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if len(params) == 0 {
			params = JSONSchemaObject()
		}
		var p any
		_ = json.Unmarshal(params, &p)
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  p,
			},
		})
	}
	return out
}
