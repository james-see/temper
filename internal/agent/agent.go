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
	Session   string
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

// Sidecar is an external harness Temper polls and steers.
// Hermes, Cursor, and OpenCode implement this.
type Sidecar interface {
	Agent
	Poll(ctx context.Context) ([]Ingest, error)
	Inject(ctx context.Context, prompt string) error
	SessionID() string
	LaunchLine() string
}
