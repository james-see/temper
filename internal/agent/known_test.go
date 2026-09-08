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
	if Implemented("opencode") {
		t.Fatal("opencode not wired")
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
