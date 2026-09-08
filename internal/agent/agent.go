package agent

import "context"

type Capabilities struct {
	Streaming        bool
	ToolCalls        bool
	MCP              bool
	ACP              bool
	Resume           bool
	Subagents        bool
	Worktrees        bool
	StructuredOutput bool
	ModelOverride    bool
}

type TaskRequest struct {
	RunID     string
	Prompt    string
	Workspace string
	Model     string
}

type Session struct {
	ID string
}

type Event struct {
	Type string
	Data any
}

type Agent interface {
	ID() string
	Capabilities(context.Context) (Capabilities, error)
	Start(context.Context, TaskRequest) (Session, error)
	Resume(context.Context, string) (Session, error)
	Interrupt(context.Context, string) error
	Events(context.Context, string) (<-chan Event, error)
}
