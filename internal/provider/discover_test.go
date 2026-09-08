package provider

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/james-see/temper/internal/config"
)

func TestDiscoverSkipsMLX401PrefersOllama(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			w.Write([]byte(`{"version":"0.33.3"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ollama.Close)
	mlx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"API key required","type":"authentication_error"}}`))
	}))
	t.Cleanup(mlx.Close)

	cfg := config.Defaults()
	o := cfg.Providers["ollama"]
	o.URL = ollama.URL
	cfg.Providers["ollama"] = o
	m := cfg.Providers["local-mlx"]
	m.URL = mlx.URL
	cfg.Providers["local-mlx"] = m

	st := Discover(t.Context(), cfg)
	c, ok := st.ByID("local-mlx")
	if !ok || c.Usable {
		t.Fatalf("mlx should be unusable: %+v", c)
	}
	best, ok := st.BestCandidate()
	if !ok || best.ID != "ollama" {
		t.Fatalf("preferred %+v ok=%v", best, ok)
	}
}

func TestDiscoverCloudKeyWins(t *testing.T) {
	t.Setenv("OLLAMA_API_KEY", "test-cloud-key")
	cfg := config.Defaults()
	o := cfg.Providers["ollama"]
	o.URL = "http://127.0.0.1:1"
	cfg.Providers["ollama"] = o
	m := cfg.Providers["local-mlx"]
	m.URL = "http://127.0.0.1:1"
	cfg.Providers["local-mlx"] = m

	st := Discover(t.Context(), cfg)
	best, ok := st.BestCandidate()
	if !ok || best.ID != "ollama-cloud" {
		t.Fatalf("preferred %+v ok=%v", best, ok)
	}
	if best.KeySource != "env:OLLAMA_API_KEY" {
		t.Fatalf("source %s", best.KeySource)
	}
}

func TestFindOllamaAPIKeyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OLLAMA_API_KEY", "")
	if err := os.MkdirAll(filepath.Join(home, ".ollama"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ollama", "api_key"), []byte("file-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	k, src := FindOllamaAPIKey(config.Config{})
	if k != "file-key" {
		t.Fatalf("key %q", k)
	}
	if src == "" {
		t.Fatal("source")
	}
}

func TestRetryable(t *testing.T) {
	if !Retryable(errStr("openai: 401 API key required")) {
		t.Fatal("401")
	}
	if Retryable(errStr("tool exploded")) {
		t.Fatal("not retryable")
	}
}

type errStr string

func (e errStr) Error() string { return string(e) }
