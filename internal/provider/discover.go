package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/james-see/temper/internal/config"
)

const OllamaCloudURL = "https://ollama.com"

type Candidate struct {
	ID         string
	Type       string
	URL        string
	Usable     bool
	Live       bool
	HasKey     bool
	Configured bool
	Reason     string
	Hint       string
	KeySource  string
}

type Status struct {
	Candidates []Candidate
	Best       string
}

func (s Status) Usable() []Candidate {
	var out []Candidate
	for _, c := range s.Candidates {
		if c.Usable {
			out = append(out, c)
		}
	}
	return out
}

func (s Status) ByID(id string) (Candidate, bool) {
	for _, c := range s.Candidates {
		if c.ID == id {
			return c, true
		}
	}
	return Candidate{}, false
}

func (s Status) Preferred(_ config.Config) (Candidate, bool) {
	return s.BestCandidate()
}

func (s Status) BestCandidate() (Candidate, bool) {
	if s.Best == "" {
		return Candidate{}, false
	}
	return s.ByID(s.Best)
}

func preferredOrder(cfg config.Config, st Status) []string {
	var out []string
	seen := map[string]bool{}
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	add(cfg.Temper.Preference.DefaultProvider)
	if cfg.Temper.Preference.LocalFirst {
		add("ollama")
		add("local-mlx")
		add("ollama-cloud")
	} else {
		add("ollama-cloud")
		add("ollama")
	}
	for _, id := range []string{"anthropic", "gemini", "openai", "openrouter", "local-mlx"} {
		add(id)
	}
	for _, c := range st.Candidates {
		add(c.ID)
	}
	return out
}

func Discover(ctx context.Context, cfg config.Config) Status {
	if ctx == nil {
		ctx = context.Background()
	}
	key, keySrc := FindOllamaAPIKey(cfg)
	var cands []Candidate

	cloud := Candidate{
		ID:         "ollama-cloud",
		Type:       "ollama",
		URL:        OllamaCloudURL,
		Configured: true,
		Hint:       "OLLAMA_API_KEY",
	}
	if p, ok := cfg.Providers["ollama-cloud"]; ok {
		if p.URL != "" {
			cloud.URL = p.URL
		} else if p.CloudURL != "" {
			cloud.URL = p.CloudURL
		}
	} else if p, ok := cfg.Providers["ollama"]; ok && p.CloudURL != "" {
		cloud.URL = p.CloudURL
	}
	if key != "" {
		cloud.HasKey = true
		cloud.Usable = true
		cloud.KeySource = keySrc
		cloud.Reason = "ollama cloud key"
	} else {
		cloud.Reason = "missing OLLAMA_API_KEY"
	}
	cands = append(cands, cloud)

	ollama := Candidate{
		ID:         "ollama",
		Type:       "ollama",
		URL:        "http://localhost:11434",
		Configured: true,
		Hint:       "start the Ollama daemon on :11434",
	}
	if p, ok := cfg.Providers["ollama"]; ok && p.URL != "" {
		ollama.URL = p.URL
	}
	if live, usable, reason := probeOllama(ctx, ollama.URL); live || usable {
		ollama.Live = live
		ollama.Usable = usable
		ollama.Reason = reason
	} else {
		ollama.Reason = reason
	}
	cands = append(cands, ollama)

	mlx := Candidate{
		ID:         "local-mlx",
		Type:       "omlx",
		URL:        "http://localhost:8000",
		Configured: true,
		Hint:       "oMLX API key or OPENAI_API_KEY",
	}
	if p, ok := cfg.Providers["local-mlx"]; ok && p.URL != "" {
		mlx.URL = p.URL
	}
	mlxKey := ""
	if p, ok := cfg.Providers["local-mlx"]; ok {
		mlxKey = p.AuthKey()
	}
	if mlxKey != "" {
		mlx.HasKey = true
		mlx.KeySource = "config:providers.local-mlx"
	}
	live, usable, reason := probeCompat(ctx, mlx.URL, mlxKey != "")
	mlx.Live = live
	mlx.Usable = usable
	mlx.Reason = reason
	cands = append(cands, mlx)

	cands = append(cands, envProvider(cfg, "anthropic", "anthropic", "ANTHROPIC_API_KEY"))
	cands = append(cands, envProvider(cfg, "gemini", "gemini", "GEMINI_API_KEY"))
	cands = append(cands, envProvider(cfg, "openai", "openai", "OPENAI_API_KEY"))
	cands = append(cands, envProvider(cfg, "openrouter", "openrouter", "OPENROUTER_API_KEY"))

	st := Status{Candidates: cands}
	rank := preferredOrder(cfg, st)
	for _, id := range rank {
		if c, ok := st.ByID(id); ok && c.Usable {
			st.Best = c.ID
			break
		}
	}
	if st.Best == "" {
		for _, c := range st.Candidates {
			if c.Usable {
				st.Best = c.ID
				break
			}
		}
	}
	return st
}

func envProvider(cfg config.Config, id, typ, envName string) Candidate {
	c := Candidate{ID: id, Type: typ, Configured: true, Hint: envName, Reason: "missing " + envName}
	if p, ok := cfg.Providers[id]; ok {
		c.URL = p.URL
		if p.AuthKey() != "" {
			c.HasKey = true
			c.Usable = true
			c.Reason = "env/config key"
			c.KeySource = "config:providers." + id
			if os.Getenv(envName) != "" {
				c.KeySource = "env:" + envName
			}
		}
	}
	return c
}

func FindOllamaAPIKey(cfg config.Config) (string, string) {
	if v := strings.TrimSpace(os.Getenv("OLLAMA_API_KEY")); v != "" {
		return v, "env:OLLAMA_API_KEY"
	}
	for _, name := range []string{"ollama-cloud", "ollama"} {
		if p, ok := cfg.Providers[name]; ok {
			if k := strings.TrimSpace(p.AuthKey()); k != "" {
				src := "config:providers." + name + ".api_key"
				return k, src
			}
		}
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return "", ""
	}
	files := []string{
		filepath.Join(home, ".ollama", "api_key"),
		filepath.Join(home, ".config", "ollama", "api_key"),
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if k := strings.TrimSpace(string(b)); k != "" {
			return k, "file:" + f
		}
	}
	cfgFiles := []string{
		filepath.Join(home, ".ollama", "config.json"),
		filepath.Join(home, ".config", "ollama", "config.json"),
	}
	for _, f := range cfgFiles {
		if k, ok := readJSONKey(f, "api_key", "apiKey", "key"); ok {
			return k, "file:" + f
		}
	}
	return "", ""
}

func readJSONKey(path string, keys ...string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", false
	}
	for _, k := range keys {
		v, ok := raw[k]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s != "" && k != "last_selection" {
			return s, true
		}
	}
	return "", false
}

func probeOllama(ctx context.Context, base string) (live, usable bool, reason string) {
	code, _, err := probeGET(ctx, strings.TrimRight(base, "/")+"/api/version")
	if err != nil {
		return false, false, "daemon not reachable"
	}
	if code >= 200 && code < 300 {
		return true, true, "local daemon"
	}
	return true, false, httpReason(code)
}

func probeCompat(ctx context.Context, base string, hasKey bool) (live, usable bool, reason string) {
	base = strings.TrimRight(base, "/")
	urls := []string{base + "/v1/models", base + "/models", base + "/"}
	var lastErr error
	var lastCode int
	for _, u := range urls {
		code, body, err := probeGET(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		lastCode = code
		if code == http.StatusUnauthorized || code == http.StatusForbidden || looksLikeKeyRequired(body) {
			if hasKey {
				return true, true, "local server + key"
			}
			return true, false, "API key required"
		}
		if code >= 200 && code < 300 {
			return true, true, "local server"
		}
	}
	if lastErr != nil && lastCode == 0 {
		return false, false, "not reachable"
	}
	if lastCode > 0 {
		return true, false, httpReason(lastCode)
	}
	return false, false, "not reachable"
}

func looksLikeKeyRequired(body []byte) bool {
	s := strings.ToLower(string(body))
	return strings.Contains(s, "api key required") || strings.Contains(s, "authentication_error")
}

func httpReason(code int) string {
	if code == 0 {
		return "no response"
	}
	return strings.TrimSpace(http.StatusText(code))
}

func probeGET(ctx context.Context, url string) (int, []byte, error) {
	cctx, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := (&http.Client{Timeout: 350 * time.Millisecond}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, b, nil
}

func Retryable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, p := range []string{
		"401", "403", "api key required", "unauthorized", "missing key",
		"connection refused", "connection reset", "no such host",
		"network is unreachable", "i/o timeout", "timeout",
	} {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func DefaultModel(cfg config.Config, id string) string {
	p, ok := cfg.Providers[id]
	typ := id
	if ok {
		typ = p.Type
	}
	switch strings.ToLower(typ) {
	case "omlx":
		return "local"
	case "ollama":
		if id == "ollama-cloud" || strings.Contains(p.URL, "ollama.com") {
			return "gpt-oss:120b"
		}
		return "llama3.2"
	case "openai", "openrouter":
		return "gpt-4.1-mini"
	case "anthropic":
		return "claude-sonnet-4-5"
	case "gemini":
		return "gemini-2.5-flash"
	default:
		return ""
	}
}

func ModelNames(ctx context.Context, cfg config.Config, id string) ([]string, error) {
	p, err := Instant(cfg, id)
	if err != nil {
		return nil, err
	}
	ms, err := p.Models(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

func Instant(cfg config.Config, id string) (Provider, error) {
	if id == "" {
		return nil, fmt.Errorf("no provider selected")
	}
	p, ok := cfg.Providers[id]
	if !ok {
		if id == "ollama-cloud" {
			p = config.Provider{Type: "ollama", URL: OllamaCloudURL, CloudURL: OllamaCloudURL}
		} else {
			return nil, fmt.Errorf("unknown provider %q", id)
		}
	}
	if id == "ollama-cloud" && p.URL == "" {
		p.URL = OllamaCloudURL
	}
	if (id == "ollama" || id == "ollama-cloud") && p.AuthKey() == "" {
		if k, _ := FindOllamaAPIKey(cfg); k != "" {
			p.Key = k
			p.APIKey = k
		}
	}
	return New(id, p)
}

func NextUsable(cfg config.Config, tried map[string]bool) (Candidate, bool) {
	st := Discover(context.Background(), cfg)
	order := preferredOrder(cfg, st)
	for _, id := range order {
		if tried[id] {
			continue
		}
		if c, ok := st.ByID(id); ok && c.Usable {
			return c, true
		}
	}
	return Candidate{}, false
}
