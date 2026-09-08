package provider

import "context"

type Model struct {
	ID string
}

type Capabilities struct {
	ContextWindow   int
	MaxOutputTokens int
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

type Message struct {
	Role    string
	Content string
}

type Request struct {
	Model    string
	Messages []Message
}

type Event struct {
	Type string
	Data any
}

type Provider interface {
	ID() string
	Models(context.Context) ([]Model, error)
	Capabilities(context.Context, Model) (Capabilities, error)
	Generate(context.Context, Request) (<-chan Event, error)
}
