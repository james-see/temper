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

type Ollama struct {
	id  string
	url string
	key string
}

func NewOllama(id, url, key string) *Ollama {
	if url == "" {
		if id == "ollama-cloud" {
			url = OllamaCloudURL
		} else {
			url = "http://localhost:11434"
		}
	}
	return &Ollama{id: id, url: strings.TrimRight(url, "/"), key: key}
}

func (p *Ollama) ID() string { return p.id }

func (p *Ollama) Models(ctx context.Context) ([]Model, error) {
	b, code, err := DoJSON(ctx, http.MethodGet, p.url+"/api/tags", p.key, "", nil, nil)
	if err != nil {
		return nil, err
	}
	if code >= 300 {
		return nil, fmt.Errorf("ollama tags: %d %s", code, b)
	}
	var resp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	out := make([]Model, 0, len(resp.Models))
	for _, m := range resp.Models {
		out = append(out, Model{ID: m.Name})
	}
	return out, nil
}

func (p *Ollama) Capabilities(_ context.Context, _ Model) (Capabilities, error) {
	local := strings.Contains(p.url, "localhost") || strings.Contains(p.url, "127.0.0.1")
	return Capabilities{Tools: true, Streaming: true, Local: local}, nil
}

func (p *Ollama) Generate(ctx context.Context, req Request) (<-chan Event, error) {
	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		if err := p.stream(ctx, req, ch); err != nil {
			ch <- Event{Type: "error", Data: err.Error()}
		}
	}()
	return ch, nil
}

func (p *Ollama) stream(ctx context.Context, req Request, ch chan<- Event) error {
	body := map[string]any{
		"model":    req.Model,
		"messages": ollamaMessages(req.Messages),
		"stream":   true,
	}
	if len(req.Tools) > 0 {
		body["tools"] = ollamaTools(req.Tools)
	}
	if req.MaxTokens > 0 {
		body["options"] = map[string]any{"num_predict": req.MaxTokens}
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.key)
	}
	resp, err := HTTPClient().Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama: %d %s", resp.StatusCode, b)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var content strings.Builder
	var calls []ToolCall
	var usage Usage
	for sc.Scan() {
		var chunk struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			Done            bool `json:"done"`
			PromptEvalCount int  `json:"prompt_eval_count"`
			EvalCount       int  `json:"eval_count"`
		}
		if err := json.Unmarshal(sc.Bytes(), &chunk); err != nil {
			continue
		}
		if chunk.Message.Content != "" {
			content.WriteString(chunk.Message.Content)
			ch <- Event{Type: "token", Data: chunk.Message.Content}
		}
		for _, tc := range chunk.Message.ToolCalls {
			args := string(tc.Function.Arguments)
			call := ToolCall{ID: fmt.Sprintf("ollama-%s", tc.Function.Name), Name: tc.Function.Name, Arguments: args}
			calls = append(calls, call)
			ch <- Event{Type: "tool_call", Data: call}
		}
		if chunk.Done {
			usage = Usage{PromptTokens: chunk.PromptEvalCount, CompletionTokens: chunk.EvalCount}
		}
	}
	if usage.PromptTokens+usage.CompletionTokens > 0 {
		ch <- Event{Type: "usage", Data: usage}
	}
	ch <- Event{Type: "done", Data: Result{Content: content.String(), ToolCalls: calls, Usage: usage}}
	return sc.Err()
}

func ollamaMessages(msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		role := m.Role
		if role == "tool" {
			role = "tool"
		}
		out = append(out, map[string]any{"role": role, "content": m.Content})
	}
	return out
}

func ollamaTools(tools []ToolSpec) []map[string]any {
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

func (p *Ollama) ChatJSON(ctx context.Context, model, prompt string, maxTokens int) (string, Usage, error) {
	body := map[string]any{
		"model":  model,
		"stream": false,
		"format": "json",
		"think":  false,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	if maxTokens > 0 {
		body["options"] = map[string]any{"num_predict": maxTokens}
	}
	raw, _ := json.Marshal(body)
	b, code, err := DoJSON(ctx, http.MethodPost, p.url+"/api/chat", p.key, "", bytes.NewReader(raw), nil)
	if err != nil {
		return "", Usage{}, err
	}
	if code >= 300 {
		return "", Usage{}, fmt.Errorf("ollama chat: %d %s", code, b)
	}
	var resp struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return "", Usage{}, err
	}
	return resp.Message.Content, Usage{PromptTokens: resp.PromptEvalCount, CompletionTokens: resp.EvalCount}, nil
}
