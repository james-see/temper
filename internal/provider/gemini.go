package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type Gemini struct {
	id  string
	url string
	key string
}

func NewGemini(id, url, key string) *Gemini {
	if url == "" {
		url = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &Gemini{id: id, url: strings.TrimRight(url, "/"), key: key}
}

func (p *Gemini) ID() string { return p.id }

func (p *Gemini) Models(_ context.Context) ([]Model, error) {
	return []Model{{ID: "gemini-2.5-flash"}, {ID: "gemini-2.5-pro"}}, nil
}

func (p *Gemini) Capabilities(_ context.Context, _ Model) (Capabilities, error) {
	return Capabilities{Tools: true, Streaming: false}, nil
}

func (p *Gemini) Generate(ctx context.Context, req Request) (<-chan Event, error) {
	ch := make(chan Event, 16)
	go func() {
		defer close(ch)
		if err := p.complete(ctx, req, ch); err != nil {
			ch <- Event{Type: "error", Data: err.Error()}
		}
	}()
	return ch, nil
}

func (p *Gemini) complete(ctx context.Context, req Request, ch chan<- Event) error {
	model := req.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}
	endpoint := fmt.Sprintf("%s/models/%s:generateContent?key=%s", p.url, url.PathEscape(model), url.QueryEscape(p.key))
	body := map[string]any{
		"contents": geminiContents(req.Messages),
	}
	if sys := geminiSystem(req.Messages); sys != "" {
		body["systemInstruction"] = map[string]any{
			"parts": []map[string]string{{"text": sys}},
		}
	}
	if len(req.Tools) > 0 {
		body["tools"] = []map[string]any{{"functionDeclarations": geminiTools(req.Tools)}}
	}
	if req.MaxTokens > 0 {
		body["generationConfig"] = map[string]any{"maxOutputTokens": req.MaxTokens}
	}
	raw, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := HTTPClient().Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var decoded struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		msg := fmt.Sprintf("gemini: %d", resp.StatusCode)
		if decoded.Error != nil {
			msg += " " + decoded.Error.Message
		}
		return fmt.Errorf("%s", msg)
	}
	var content strings.Builder
	var calls []ToolCall
	if len(decoded.Candidates) > 0 {
		for i, part := range decoded.Candidates[0].Content.Parts {
			if part.Text != "" {
				content.WriteString(part.Text)
				ch <- Event{Type: "token", Data: part.Text}
			}
			if part.FunctionCall != nil {
				tc := ToolCall{
					ID:        fmt.Sprintf("gemini-%d", i),
					Name:      part.FunctionCall.Name,
					Arguments: string(part.FunctionCall.Args),
				}
				calls = append(calls, tc)
				ch <- Event{Type: "tool_call", Data: tc}
			}
		}
	}
	usage := Usage{
		PromptTokens:     decoded.UsageMetadata.PromptTokenCount,
		CompletionTokens: decoded.UsageMetadata.CandidatesTokenCount,
	}
	ch <- Event{Type: "usage", Data: usage}
	ch <- Event{Type: "done", Data: Result{Content: content.String(), ToolCalls: calls, Usage: usage}}
	return nil
}

func geminiSystem(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		if m.Role == "system" {
			b.WriteString(m.Content)
		}
	}
	return b.String()
}

func geminiContents(msgs []Message) []map[string]any {
	var out []map[string]any
	for _, m := range msgs {
		if m.Role == "system" {
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		var parts []map[string]any
		if m.Role == "tool" {
			parts = append(parts, map[string]any{
				"functionResponse": map[string]any{
					"name":     m.Name,
					"response": map[string]any{"result": m.Content},
				},
			})
			role = "user"
		} else if len(m.ToolCalls) > 0 {
			if m.Content != "" {
				parts = append(parts, map[string]any{"text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args any
				_ = json.Unmarshal([]byte(tc.Arguments), &args)
				parts = append(parts, map[string]any{
					"functionCall": map[string]any{"name": tc.Name, "args": args},
				})
			}
			role = "model"
		} else {
			parts = append(parts, map[string]any{"text": m.Content})
		}
		out = append(out, map[string]any{"role": role, "parts": parts})
	}
	return out
}

func geminiTools(tools []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if len(params) == 0 {
			params = JSONSchemaObject()
		}
		var schema any
		_ = json.Unmarshal(params, &schema)
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"parameters":  schema,
		})
	}
	return out
}
