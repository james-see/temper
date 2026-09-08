package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func ProjectConfigFile(files []string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	want := map[string]bool{
		canon(filepath.Join(cwd, "temper.yaml")):            true,
		canon(filepath.Join(cwd, ".temper", "config.yaml")): true,
	}
	for _, f := range files {
		if want[canon(f)] {
			abs, err := filepath.Abs(f)
			if err != nil {
				return f
			}
			return abs
		}
	}
	return ""
}

func PersistReflexMode(path, mode string) error {
	if path == "" {
		return nil
	}
	mode = Reflex{Mode: mode}.EffectiveMode()
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var overlay map[string]any
	if err := yaml.Unmarshal(raw, &overlay); err != nil {
		return err
	}
	if overlay == nil {
		overlay = map[string]any{}
	}
	reflex, _ := overlay["reflex"].(map[string]any)
	if reflex == nil {
		reflex = map[string]any{}
	}
	reflex["mode"] = mode
	overlay["reflex"] = reflex
	out, err := yaml.Marshal(overlay)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func NormalizeReflexMode(mode string) string {
	return Reflex{Mode: strings.TrimSpace(mode)}.EffectiveMode()
}

func canon(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}
