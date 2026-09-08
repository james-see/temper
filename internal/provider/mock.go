package provider

import "context"

type Mock struct {
	Name    string
	Handler func(Request) Result
	Err     error
}

func (m *Mock) ID() string { return m.Name }

func (m *Mock) Models(context.Context) ([]Model, error) {
	return []Model{{ID: "mock"}}, nil
}

func (m *Mock) Capabilities(context.Context, Model) (Capabilities, error) {
	return Capabilities{Tools: true, Streaming: true, Local: true}, nil
}

func (m *Mock) Generate(_ context.Context, req Request) (<-chan Event, error) {
	ch := make(chan Event, 8)
	go func() {
		defer close(ch)
		if m.Err != nil {
			ch <- Event{Type: "error", Data: m.Err.Error()}
			return
		}
		var r Result
		if m.Handler != nil {
			r = m.Handler(req)
		} else {
			r = Result{Content: "ok"}
		}
		if r.Content != "" {
			ch <- Event{Type: "token", Data: r.Content}
		}
		for _, tc := range r.ToolCalls {
			ch <- Event{Type: "tool_call", Data: tc}
		}
		ch <- Event{Type: "usage", Data: r.Usage}
		ch <- Event{Type: "done", Data: r}
	}()
	return ch, nil
}
