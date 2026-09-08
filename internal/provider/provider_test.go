package provider

import "testing"

func TestCollect(t *testing.T) {
	ch := make(chan Event, 4)
	ch <- Event{Type: "token", Data: "hi"}
	ch <- Event{Type: "tool_call", Data: ToolCall{Name: "shell"}}
	ch <- Event{Type: "usage", Data: Usage{PromptTokens: 1, CompletionTokens: 2}}
	ch <- Event{Type: "done", Data: Result{Content: "hi", ToolCalls: []ToolCall{{Name: "shell"}}, Usage: Usage{PromptTokens: 1, CompletionTokens: 2}}}
	close(ch)
	r := Collect(ch)
	if r.Content != "hi" || len(r.ToolCalls) != 1 || r.Usage.CompletionTokens != 2 {
		t.Fatalf("%+v", r)
	}
}

func TestNewTypes(t *testing.T) {
	if NewOpenAI("openai", "", "").ID() != "openai" {
		t.Fatal("openai")
	}
	if NewOllama("ollama", "", "").ID() != "ollama" {
		t.Fatal("ollama")
	}
	if NewAnthropic("anthropic", "", "").ID() != "anthropic" {
		t.Fatal("anthropic")
	}
	if NewGemini("gemini", "", "").ID() != "gemini" {
		t.Fatal("gemini")
	}
}
