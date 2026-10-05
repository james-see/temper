package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
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

func TestDetectorDefaults(t *testing.T) {
	d := Defaults().Reflex.Detectors
	if d.RepoStagnation.Actions != 4 {
		t.Fatalf("repo_stagnation: %+v", d.RepoStagnation)
	}
	if d.TestStagnation.Threshold != 3 {
		t.Fatalf("test_stagnation: %+v", d.TestStagnation)
	}
	if d.CostBurn.USD != 0 {
		t.Fatalf("cost_burn must default disabled: %+v", d.CostBurn)
	}
	if d.PlanWithoutExec.Actions != 3 {
		t.Fatalf("plan_without_exec: %+v", d.PlanWithoutExec)
	}
	if !d.EditOscillation.Enabled {
		t.Fatalf("edit_oscillation must default on: %+v", d.EditOscillation)
	}
}

func TestRecoveryLadderDefaults(t *testing.T) {
	r := Defaults().Reflex
	if r.RecoveryMaxAttempts != 10 {
		t.Fatalf("max attempts %d", r.RecoveryMaxAttempts)
	}
	if r.RecoveryBackoffSeconds != 0 {
		t.Fatalf("backoff must default disabled: %d", r.RecoveryBackoffSeconds)
	}
	var actions []string
	for _, s := range r.Recovery {
		actions = append(actions, s.Action)
	}
	want := []string{"replan", "critic", "switch_model", "switch_agent", "rollback", "fork", "human"}
	if len(actions) != len(want) {
		t.Fatalf("ladder %v", actions)
	}
	for i := range want {
		if actions[i] != want[i] {
			t.Fatalf("ladder %v", actions)
		}
	}
	if (Reflex{Mode: "off"}).EffectiveMode() != ReflexOff {
		t.Fatal("off mode must pass through")
	}
	if (Reflex{Mode: "bogus"}).EffectiveMode() != ReflexAuto {
		t.Fatal("unknown mode must default to auto")
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

func TestWorkspaceBranchFields(t *testing.T) {
	var c Config
	if err := yaml.Unmarshal([]byte("root: /tmp/w\nbranch_prefix: task/\nbase_branch: develop\n"), &c.Workspace); err != nil {
		t.Fatal(err)
	}
	if c.Workspace.BranchPrefix != "task/" || c.Workspace.BaseBranch != "develop" || c.Workspace.Root != "/tmp/w" {
		t.Fatalf("%+v", c.Workspace)
	}
}

func TestArbiterPolicyFields(t *testing.T) {
	var c Config
	yml := "escalation: [ollama, openai]\npolicies:\n- allow_providers: [ollama, openai]\n  deny_providers: [gemini]\n  allow_agents: [native]\n  deny_agents: [cursor]\n"
	if err := yaml.Unmarshal([]byte(yml), &c.Arbiter); err != nil {
		t.Fatal(err)
	}
	if len(c.Arbiter.Escalation) != 2 || c.Arbiter.Escalation[0] != "ollama" {
		t.Fatalf("%+v", c.Arbiter.Escalation)
	}
	p := c.Arbiter.Policies[0]
	if len(p.AllowProviders) != 2 || len(p.DenyProviders) != 1 || p.DenyProviders[0] != "gemini" {
		t.Fatalf("%+v", p)
	}
	if len(p.AllowAgents) != 1 || len(p.DenyAgents) != 1 || p.DenyAgents[0] != "cursor" {
		t.Fatalf("%+v", p)
	}
	if len(Defaults().Arbiter.Escalation) != 0 || len(Defaults().Arbiter.Policies) != 0 {
		t.Fatal("arbiter policy must default empty")
	}
}

func TestRoutingExampleLoads(t *testing.T) {
	loaded, err := Load(Flags{Config: "../../examples/routing.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"zai", "zai-coding", "ollama", "local-mlx", "openai", "anthropic"} {
		p, ok := loaded.Config.Providers[id]
		if !ok || p.Type == "" {
			t.Fatalf("missing provider %s", id)
		}
	}
	if loaded.Config.Providers["zai"].URL != "https://api.z.ai/api/paas/v4" {
		t.Fatalf("%+v", loaded.Config.Providers["zai"])
	}
	if len(loaded.Config.Arbiter.Escalation) != 5 {
		t.Fatalf("%+v", loaded.Config.Arbiter.Escalation)
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
