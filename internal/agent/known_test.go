package agent

import (
	"testing"

	"github.com/james-see/temper/internal/config"
)

func TestResolveKnownAndConfigured(t *testing.T) {
	cfg := config.Defaults()
	name, typ, ok := Resolve("hermes", cfg)
	if !ok || name != "hermes" || typ != "hermes" {
		t.Fatalf("%s %s %v", name, typ, ok)
	}
	if !Implemented("hermes") || IsNative("hermes") || !IsExternal("hermes") {
		t.Fatal("hermes flags")
	}
	if !IsNative("native") || IsExternal("native") {
		t.Fatal("native flags")
	}
	if !Implemented("opencode") || !IsExternal("opencode") || IsNative("opencode") {
		t.Fatal("opencode flags")
	}
	if !Implemented("muse") || !IsExternal("muse") || TypeOf("muse-code") != "muse" {
		t.Fatal("muse flags")
	}
	if !Implemented("goose") || !IsExternal("goose") || TypeOf("goose") != "goose" {
		t.Fatal("goose flags")
	}
	if !Implemented("claude") || !Implemented("claude-code") || TypeOf("claude") != "claude-code" {
		t.Fatal("claude-code flags")
	}
	if !Implemented("codex") || !IsExternal("codex") || TypeOf("codex") != "codex" {
		t.Fatal("codex flags")
	}
	if !Implemented("cursor") {
		t.Fatal("cursor wired")
	}
	cfg.Agents = map[string]config.Agent{"mine": {Type: "hermes"}}
	name, typ, ok = Resolve("mine", cfg)
	if !ok || typ != "hermes" {
		t.Fatalf("configured %s %s %v", name, typ, ok)
	}
	if _, _, ok = Resolve("not-an-agent", cfg); ok {
		t.Fatal("unknown")
	}
}
