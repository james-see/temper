package cli

import (
	"strings"
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
	_, _, err = splitAgentArgs([]string{"opencode"}, cfg, "")
	if err == nil || !strings.Contains(err.Error(), "not wired") {
		t.Fatalf("%v", err)
	}
	id, goal, err = splitAgentArgs([]string{"native", "do", "it"}, cfg, "")
	if err != nil || id != "native" || goal != "do it" {
		t.Fatalf("%s %q %v", id, goal, err)
	}
	id, goal, err = splitAgentArgs([]string{"still a goal"}, cfg, "hermes")
	if err != nil || id != "hermes" || goal != "still a goal" {
		t.Fatalf("flag %s %q %v", id, goal, err)
	}
}
