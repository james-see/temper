package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type Model struct {
	ID string
}

type Capabilities struct {
	ContextWindow    int
	MaxOutputTokens  int
	Tools            bool
	ParallelTools    bool
	Vision           bool
	StructuredOutput bool
	Reasoning        bool
	Embeddings       bool
	PromptCaching    bool
	Streaming        bool
	Local            bool
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type Message struct {
	Role       string
	Content    string
	Name       string
	ToolCallID string
	ToolCalls  []ToolCall
}

type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

type Request struct {
	Model       string
	Messages    []Message
	Tools       []ToolSpec
	MaxTokens   int
	Temperature float64
}

type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

type Event struct {
	Type string
	Data any
}

type Result struct {
	Content   string
	ToolCalls []ToolCall
	Usage     Usage
}

type Provider interface {
	ID() string
	Models(context.Context) ([]Model, error)
	Capabilities(context.Context, Model) (Capabilities, error)
	Generate(context.Context, Request) (<-chan Event, error)
}

func Collect(ch <-chan Event) Result {
	var r Result
	for ev := range ch {
		switch ev.Type {
		case "token":
			if s, ok := ev.Data.(string); ok {
				r.Content += s
			}
		case "tool_call":
			if tc, ok := ev.Data.(ToolCall); ok {
				r.ToolCalls = append(r.ToolCalls, tc)
			}
		case "usage":
			if u, ok := ev.Data.(Usage); ok {
				r.Usage = u
			}
		case "done":
			if res, ok := ev.Data.(Result); ok {
				return res
			}
		}
	}
	return r
}

func HTTPClient() *http.Client {
	return &http.Client{Timeout: 120 * time.Second}
}

// StreamClient has no request timeout. Generate streams (local prefill
// of a 27B model can exceed two minutes) are cancelled via context.
func StreamClient() *http.Client {
	return &http.Client{}
}

func DoJSON(ctx context.Context, method, url, key, bearerFmt string, body io.Reader, extra map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		if strings.Contains(bearerFmt, "%s") {
			req.Header.Set("Authorization", bearerFmt)
		} else if bearerFmt != "" {
			req.Header.Set(bearerFmt, key)
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := HTTPClient().Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return b, resp.StatusCode, err
}

func JSONSchemaObject() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
