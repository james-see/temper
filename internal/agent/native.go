package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/james-see/temper/internal/provider"
)

type Native struct {
	prov     provider.Provider
	model    string
	tools    []Tool
	messages []provider.Message
}

func NewNative(p provider.Provider, model, workspace string, timeout time.Duration, deny []string) *Native {
	return &Native{
		prov:  p,
		model: model,
		tools: NativeTools(workspace, timeout, deny),
		messages: []provider.Message{{
			Role: "system",
			Content: "You are Temper's native coding agent. Use tools to make observable progress in the workspace. " +
				"Prefer small edits, run tests, and stop when the goal is verified. Workspace: " + workspace,
		}},
	}
}

func (n *Native) ID() string { return "native" }

func (n *Native) Capabilities(context.Context) (Capabilities, error) {
	return Capabilities{Streaming: true, ToolCalls: true, Worktrees: true, StructuredOutput: true, ModelOverride: true}, nil
}

func (n *Native) Start(_ context.Context, req TaskRequest) (Session, error) {
	n.messages = append(n.messages, provider.Message{Role: "user", Content: req.Prompt})
	if req.Model != "" {
		n.model = req.Model
	}
	return Session{ID: req.RunID}, nil
}

func (n *Native) Resume(context.Context, string) (Session, error) {
	return Session{}, fmt.Errorf("resume not implemented in v0.1")
}

func (n *Native) Interrupt(context.Context, string) error { return nil }

func (n *Native) Events(context.Context, string) (<-chan Event, error) {
	return nil, fmt.Errorf("use Step")
}

type StepResult struct {
	Content   string
	ToolCalls []provider.ToolCall
	ToolOut   []ToolOutcome
	Usage     provider.Usage
	Done      bool
	Err       error
}

type ToolOutcome struct {
	Name     string
	Args     string
	Output   string
	Files    []string
	Err      string
	Duration time.Duration
}

func (n *Native) Step(ctx context.Context) StepResult {
	ch, err := n.prov.Generate(ctx, provider.Request{
		Model:    n.model,
		Messages: n.messages,
		Tools:    Specs(n.tools),
	})
	if err != nil {
		return StepResult{Err: err}
	}
	var content strings.Builder
	var calls []provider.ToolCall
	var usage provider.Usage
	var genErr error
	for ev := range ch {
		switch ev.Type {
		case "token":
			if s, ok := ev.Data.(string); ok {
				content.WriteString(s)
			}
		case "tool_call":
			if tc, ok := ev.Data.(provider.ToolCall); ok {
				calls = append(calls, tc)
			}
		case "usage":
			if u, ok := ev.Data.(provider.Usage); ok {
				usage = u
			}
		case "done":
			if r, ok := ev.Data.(provider.Result); ok {
				if r.Content != "" {
					content.Reset()
					content.WriteString(r.Content)
				}
				if len(r.ToolCalls) > 0 {
					calls = r.ToolCalls
				}
				if r.Usage.PromptTokens+r.Usage.CompletionTokens > 0 {
					usage = r.Usage
				}
			}
		case "error":
			genErr = fmt.Errorf("%v", ev.Data)
		}
	}
	if genErr != nil {
		return StepResult{Err: genErr, Usage: usage}
	}
	text := content.String()
	if len(calls) == 0 {
		n.messages = append(n.messages, provider.Message{Role: "assistant", Content: text})
		return StepResult{Content: text, Usage: usage, Done: true}
	}
	n.messages = append(n.messages, provider.Message{Role: "assistant", Content: text, ToolCalls: calls})
	var outs []ToolOutcome
	for _, tc := range calls {
		t, ok := Lookup(n.tools, tc.Name)
		start := time.Now()
		out := ToolOutcome{Name: tc.Name, Args: tc.Arguments}
		if !ok {
			out.Err = "unknown tool"
			outs = append(outs, out)
			n.messages = append(n.messages, provider.Message{Role: "tool", Name: tc.Name, ToolCallID: tc.ID, Content: out.Err})
			continue
		}
		args := json.RawMessage(tc.Arguments)
		if !json.Valid(args) {
			args = json.RawMessage(`{}`)
		}
		result, files, err := t.Run(ctx, args)
		out.Output = result
		out.Files = files
		out.Duration = time.Since(start)
		if err != nil {
			out.Err = err.Error()
			result = err.Error() + "\n" + result
		}
		n.messages = append(n.messages, provider.Message{Role: "tool", Name: tc.Name, ToolCallID: tc.ID, Content: result})
		outs = append(outs, out)
	}
	return StepResult{Content: text, ToolCalls: calls, ToolOut: outs, Usage: usage}
}

func (n *Native) Inject(role, content string) {
	n.messages = append(n.messages, provider.Message{Role: role, Content: content})
}
