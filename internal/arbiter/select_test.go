package arbiter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/james-see/temper/internal/config"
)

func TestSelectIgnoresUnusableMLX(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"0.33.3"}`))
	}))
	t.Cleanup(ollama.Close)
	mlx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"API key required"}}`))
	}))
	t.Cleanup(mlx.Close)

	cfg := config.Defaults()
	o := cfg.Providers["ollama"]
	o.URL = ollama.URL
	cfg.Providers["ollama"] = o
	m := cfg.Providers["local-mlx"]
	m.URL = mlx.URL
	cfg.Providers["local-mlx"] = m
	cfg.Arbiter.Policies = []config.Policy{{
		Prefer: config.PolicyPrefer{Provider: "local-mlx", Model: "local"},
	}}

	d := Select(cfg, "", "", "")
	if d.Selected.Provider == "local-mlx" {
		t.Fatalf("should not pick local-mlx: %+v", d)
	}
	if d.Selected.Provider != "ollama" {
		t.Fatalf("got %s reasons=%v", d.Selected.Provider, d.Reasons)
	}
}

func TestSelectKeepsExplicitProvider(t *testing.T) {
	cfg := config.Defaults()
	d := Select(cfg, "native", "openai", "gpt-4.1-mini")
	if d.Selected.Provider != "openai" {
		t.Fatalf("%s", d.Selected.Provider)
	}
}
