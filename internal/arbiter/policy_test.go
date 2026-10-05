package arbiter

import (
	"strings"
	"testing"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/provider"
)

func policyStatus() provider.Status {
	return provider.Status{
		Best: "ollama",
		Candidates: []provider.Candidate{
			{ID: "ollama-cloud", Type: "ollama", Usable: true, Reason: "key"},
			{ID: "ollama", Type: "ollama", Usable: true, Reason: "live"},
			{ID: "openai", Type: "openai", Usable: true, Reason: "key"},
			{ID: "gemini", Type: "gemini", Usable: false, Reason: "no key"},
		},
	}
}

func policyCfg() config.Config {
	cfg := config.Defaults()
	cfg.Temper.Preference.DefaultProvider = ""
	return cfg
}

func hasReason(reasons []string, sub string) bool {
	for _, r := range reasons {
		if strings.Contains(r, sub) {
			return true
		}
	}
	return false
}

func TestSelectDenyBeatsExplicitProvider(t *testing.T) {
	cfg := policyCfg()
	cfg.Arbiter.Policies = []config.Policy{{DenyProviders: []string{"openai"}}}
	d := SelectWithStatus(cfg, "native", "openai", "gpt-x", policyStatus())
	if d.Selected.Provider == "openai" {
		t.Fatalf("denied provider selected: %+v", d)
	}
	if !hasReason(d.Reasons, "explicit provider openai blocked by policy") {
		t.Fatalf("missing deny reason: %v", d.Reasons)
	}
}

func TestSelectAllowRestrictsPreferred(t *testing.T) {
	cfg := policyCfg()
	cfg.Arbiter.Policies = []config.Policy{{AllowProviders: []string{"openai"}}}
	d := SelectWithStatus(cfg, "", "", "", policyStatus())
	if d.Selected.Provider != "openai" {
		t.Fatalf("got %s reasons=%v", d.Selected.Provider, d.Reasons)
	}
	if !hasReason(d.Reasons, "preferred ollama blocked by policy") {
		t.Fatalf("missing allow reason: %v", d.Reasons)
	}
}

func TestSelectDenyBeatsAllow(t *testing.T) {
	cfg := policyCfg()
	cfg.Arbiter.Policies = []config.Policy{{
		AllowProviders: []string{"ollama", "openai"},
		DenyProviders:  []string{"ollama"},
	}}
	d := SelectWithStatus(cfg, "", "", "", policyStatus())
	if d.Selected.Provider != "openai" {
		t.Fatalf("got %s reasons=%v", d.Selected.Provider, d.Reasons)
	}
}

func TestSelectDeniedAgentFallsBackToNative(t *testing.T) {
	cfg := policyCfg()
	cfg.Arbiter.Policies = []config.Policy{{DenyAgents: []string{"cursor"}}}
	d := SelectWithStatus(cfg, "cursor", "", "", policyStatus())
	if d.Selected.Agent != "native" {
		t.Fatalf("got agent %s reasons=%v", d.Selected.Agent, d.Reasons)
	}
	if !hasReason(d.Reasons, "agent cursor blocked by policy") {
		t.Fatalf("missing agent deny reason: %v", d.Reasons)
	}
}

func TestSelectDenyAllProvidersLeavesEmpty(t *testing.T) {
	cfg := policyCfg()
	cfg.Arbiter.Policies = []config.Policy{{
		DenyProviders: []string{"ollama-cloud", "ollama", "openai", "gemini"},
	}}
	d := SelectWithStatus(cfg, "", "", "", policyStatus())
	if d.Selected.Provider != "" {
		t.Fatalf("denied provider selected: %+v", d)
	}
	if !hasReason(d.Reasons, "blocked by policy") {
		t.Fatalf("missing deny reason: %v", d.Reasons)
	}
}

func TestSelectLocalFirstEnforcedOverCloud(t *testing.T) {
	cfg := policyCfg()
	cfg.Temper.Preference.LocalFirst = true
	st := policyStatus()
	st.Best = "ollama-cloud"
	d := SelectWithStatus(cfg, "", "", "", st)
	if d.Selected.Provider != "ollama" {
		t.Fatalf("got %s reasons=%v", d.Selected.Provider, d.Reasons)
	}
	if !hasReason(d.Reasons, "local_first enforced: ollama over ollama-cloud") {
		t.Fatalf("missing enforcement reason: %v", d.Reasons)
	}
}

func TestSelectLocalFirstKeepsCloudWhenNoLocalUsable(t *testing.T) {
	cfg := policyCfg()
	cfg.Temper.Preference.LocalFirst = true
	st := policyStatus()
	st.Best = "ollama-cloud"
	for i, c := range st.Candidates {
		if c.ID == "ollama" || c.ID == "local-mlx" {
			st.Candidates[i].Usable = false
		}
	}
	d := SelectWithStatus(cfg, "", "", "", st)
	if d.Selected.Provider != "ollama-cloud" {
		t.Fatalf("got %s reasons=%v", d.Selected.Provider, d.Reasons)
	}
	if hasReason(d.Reasons, "local_first enforced") {
		t.Fatalf("spurious enforcement: %v", d.Reasons)
	}
}

func TestSelectExplicitBeatsLocalFirst(t *testing.T) {
	cfg := policyCfg()
	cfg.Temper.Preference.LocalFirst = true
	d := SelectWithStatus(cfg, "native", "openai", "", policyStatus())
	if d.Selected.Provider != "openai" {
		t.Fatalf("got %s reasons=%v", d.Selected.Provider, d.Reasons)
	}
}

func TestSelectSilentPolicyKeepsLegacyReasons(t *testing.T) {
	d := SelectWithStatus(policyCfg(), "", "", "", policyStatus())
	if d.Selected.Provider != "ollama" {
		t.Fatalf("got %s reasons=%v", d.Selected.Provider, d.Reasons)
	}
	if !hasReason(d.Reasons, "preferred=ollama (live)") {
		t.Fatalf("missing legacy reason: %v", d.Reasons)
	}
	for _, banned := range []string{"blocked by policy", "local_first enforced", "policy fallback"} {
		if hasReason(d.Reasons, banned) {
			t.Fatalf("spurious reason %q: %v", banned, d.Reasons)
		}
	}
}
