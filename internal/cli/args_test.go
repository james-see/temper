package cli

import (
	"testing"

	"github.com/james-see/temper/internal/config"
)

func TestSplitAgentArgs(t *testing.T) {
	cfg := config.Defaults()
	id, goal, err := splitAgentArgs([]string{"hermes"}, cfg, "")
	if err != nil || id != "hermes" || goal != "" {
		t.Fatalf("%s %q %v", id, goal, err)
	}
	id, goal, err = splitAgentArgs([]string{"hermes", "fix", "the", "tests"}, cfg, "")
	if err != nil || id != "hermes" || goal != "fix the tests" {
		t.Fatalf("%s %q %v", id, goal, err)
	}
	id, goal, err = splitAgentArgs([]string{"check", "hermes", "docs"}, cfg, "")
	if err != nil || id != "" || goal != "check hermes docs" {
		t.Fatalf("%s %q %v", id, goal, err)
	}
	id, goal, err = splitAgentArgs([]string{"opencode", "ship", "it"}, cfg, "")
	if err != nil || id != "opencode" || goal != "ship it" {
		t.Fatalf("opencode %s %q %v", id, goal, err)
	}
	id, goal, err = splitAgentArgs([]string{"native", "do", "it"}, cfg, "")
	if err != nil || id != "native" || goal != "do it" {
		t.Fatalf("%s %q %v", id, goal, err)
	}
	id, goal, err = splitAgentArgs([]string{"still a goal"}, cfg, "hermes")
	if err != nil || id != "hermes" || goal != "still a goal" {
		t.Fatalf("flag %s %q %v", id, goal, err)
	}
	id, goal, err = splitAgentArgs([]string{"cursor", "watch", "this"}, cfg, "")
	if err != nil || id != "cursor" || goal != "watch this" {
		t.Fatalf("cursor %s %q %v", id, goal, err)
	}
}

func TestParseCursorSession(t *testing.T) {
	_, sess, all := parseCursorSession("all")
	if sess != "" || !all {
		t.Fatalf("%q %v", sess, all)
	}
	_, sess, all = parseCursorSession("*")
	if sess != "" || !all {
		t.Fatalf("star %q %v", sess, all)
	}
	_, sess, all = parseCursorSession("abc-123")
	if sess != "abc-123" || all {
		t.Fatalf("id %q %v", sess, all)
	}
}
