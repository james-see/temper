package agent

import (
	"fmt"
	"strings"

	"github.com/james-see/temper/internal/config"
)

// Known IDs and aliases. Values are adapter types.
var Known = map[string]string{
	"native":      "temper",
	"temper":      "temper",
	"hermes":      "hermes",
	"opencode":    "opencode",
	"claude":      "claude-code",
	"claude-code": "claude-code",
	"codex":       "codex",
	"cursor":      "cursor",
}

func Normalize(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func IsKnown(id string) bool {
	_, ok := Known[Normalize(id)]
	return ok
}

func TypeOf(id string) string {
	if t, ok := Known[Normalize(id)]; ok {
		return t
	}
	return ""
}

func Implemented(id string) bool {
	switch TypeOf(id) {
	case "temper", "hermes", "cursor":
		return true
	default:
		return false
	}
}

func IsNative(id string) bool {
	return TypeOf(id) == "temper" || Normalize(id) == "" || Normalize(id) == "native"
}

func IsExternal(id string) bool {
	id = Normalize(id)
	if id == "" || IsNative(id) {
		return false
	}
	return IsKnown(id)
}

// Resolve looks up a first-token or configured agent id.
func Resolve(id string, cfg config.Config) (name, typ string, ok bool) {
	id = Normalize(id)
	if id == "" {
		return "", "", false
	}
	if t, found := Known[id]; found {
		return id, t, true
	}
	if cfg.Agents != nil {
		if a, found := cfg.Agents[id]; found && a.Type != "" {
			return id, a.Type, true
		}
	}
	return "", "", false
}

func UnimplementedError(id string) error {
	return fmt.Errorf("%s adapter is not wired yet", Normalize(id))
}
