package arbiter

import (
	"testing"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/provider"
)

func chainCfg() config.Config {
	cfg := config.Defaults()
	cfg.Temper.Preference.DefaultProvider = ""
	cfg.Arbiter.Escalation = []string{"openai", "ollama"}
	return cfg
}

func TestNextInChainHonorsEscalationOrder(t *testing.T) {
	c, reasons, ok := NextInChain(chainCfg(), map[string]bool{}, policyStatus())
	if !ok || c.ID != "openai" {
		t.Fatalf("got %+v %v %v", c, reasons, ok)
	}
	if !hasReason(reasons, "position 1/") {
		t.Fatalf("missing position reason: %v", reasons)
	}
}

func TestNextInChainSkipsTriedUnusableDenied(t *testing.T) {
	cfg := chainCfg()
	cfg.Arbiter.Policies = []config.Policy{{DenyProviders: []string{"ollama"}}}
	tried := map[string]bool{"openai": true}
	c, reasons, ok := NextInChain(cfg, tried, policyStatus())
	// openai tried, ollama denied: ollama-cloud remains.
	if !ok || c.ID != "ollama-cloud" {
		t.Fatalf("got %+v %v %v", c, reasons, ok)
	}
	for _, want := range []string{"skipped tried: openai", "chain ollama: blocked by policy"} {
		if !hasReason(reasons, want) {
			t.Fatalf("missing %q: %v", want, reasons)
		}
	}
}

func TestNextInChainUnusableNarrated(t *testing.T) {
	cfg := policyCfg() // default order reaches gemini
	st := provider.Status{
		Candidates: []provider.Candidate{
			{ID: "gemini", Usable: false, Reason: "no key"},
		},
	}
	_, reasons, ok := NextInChain(cfg, map[string]bool{}, st)
	if ok {
		t.Fatalf("expected exhaustion: %v", reasons)
	}
	if !hasReason(reasons, "chain gemini: unusable (no key)") {
		t.Fatalf("missing unusable reason: %v", reasons)
	}
}

func TestNextInChainUnknownIdNarrated(t *testing.T) {
	cfg := chainCfg()
	cfg.Arbiter.Escalation = []string{"nope", "openai"}
	c, reasons, ok := NextInChain(cfg, map[string]bool{}, policyStatus())
	if !ok || c.ID != "openai" {
		t.Fatalf("got %+v %v %v", c, reasons, ok)
	}
	if !hasReason(reasons, "chain nope: unknown provider") {
		t.Fatalf("missing unknown reason: %v", reasons)
	}
}

func TestNextInChainExhausted(t *testing.T) {
	cfg := chainCfg()
	tried := map[string]bool{"ollama-cloud": true, "ollama": true, "openai": true, "gemini": true}
	_, reasons, ok := NextInChain(cfg, tried, policyStatus())
	if ok {
		t.Fatalf("expected exhaustion: %v", reasons)
	}
	if !hasReason(reasons, "chain exhausted") {
		t.Fatalf("missing exhausted reason: %v", reasons)
	}
}

func TestNextInChainDefaultsToDiscoveryOrder(t *testing.T) {
	cfg := policyCfg() // no escalation list
	st := provider.Status{
		Candidates: []provider.Candidate{
			{ID: "anthropic", Usable: true, Reason: "key"},
		},
	}
	c, reasons, ok := NextInChain(cfg, map[string]bool{}, st)
	if !ok || c.ID != "anthropic" {
		t.Fatalf("got %+v %v %v", c, reasons, ok)
	}
}
