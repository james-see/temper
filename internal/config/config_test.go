package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsJudgeModel(t *testing.T) {
	c := Defaults()
	if c.Reflex.Judge.Model != DefaultJudgeModel {
		t.Fatalf("judge model: %s", c.Reflex.Judge.Model)
	}
	if !c.Reflex.Judge.EffectiveEnabled() || !c.Reflex.Judge.AutoPull {
		t.Fatal("judge defaults on")
	}
	if !(Judge{}).EffectiveEnabled() {
		t.Fatal("unset enabled is on")
	}
	off := false
	if (Judge{Enabled: &off}).EffectiveEnabled() {
		t.Fatal("enabled false")
	}
}

func TestMergeLaterWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	user := filepath.Join(dir, "temper")
	if err := os.MkdirAll(user, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "temper.yaml"), []byte("temper:\n  budget:\n    max_cost_per_task: 1.5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	proj := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(proj); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("temper.yaml", []byte("temper:\n  budget:\n    max_cost_per_task: 9.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Temper.Budget.MaxCostPerTask != 9.25 {
		t.Fatalf("got %v", loaded.Config.Temper.Budget.MaxCostPerTask)
	}
	if loaded.Sources["temper.budget.max_cost_per_task"] != "temper.yaml" {
		t.Fatalf("source %q", loaded.Sources["temper.budget.max_cost_per_task"])
	}
}

func TestEnvAndFlags(t *testing.T) {
	t.Setenv("TEMPER_MODEL", "from-env")
	t.Setenv("OPENAI_API_KEY", "sk-test")
	loaded, err := Load(Flags{Model: "from-flag", Agent: "native"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, model := Selected(loaded.Config)
	if model != "from-flag" {
		t.Fatalf("model %s", model)
	}
	if loaded.Config.Providers["openai"].Key != "sk-test" {
		t.Fatal("openai key not applied")
	}
}

func TestOllamaAPIKeyEnv(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "ollama-secret")
	loaded, err := Load(Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Providers["ollama"].AuthKey() != "ollama-secret" {
		t.Fatal("ollama key")
	}
	if loaded.Config.Providers["ollama-cloud"].AuthKey() != "ollama-secret" {
		t.Fatal("ollama-cloud key")
	}
	if loaded.Config.Temper.Preference.DefaultProvider != "ollama-cloud" {
		t.Fatalf("default provider %s", loaded.Config.Temper.Preference.DefaultProvider)
	}
}
