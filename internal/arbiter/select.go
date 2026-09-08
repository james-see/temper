package arbiter

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/james-see/temper/internal/config"
)

func Select(cfg config.Config, agent, provider, model string) Decision {
	if agent == "" && provider == "" && model == "" {
		agent, provider, model = config.Selected(cfg)
	}
	if agent == "" {
		agent = "native"
	}
	if provider == "" {
		provider = pickProvider(cfg)
	}
	if model == "" {
		model = defaultModel(cfg, provider)
	}
	d := Decision{
		Selected: Candidate{Agent: agent, Provider: provider, Model: model, Score: 1},
		Reasons:  []string{"static arbiter: config/flags then local-first"},
	}
	if cfg.Temper.Preference.LocalFirst {
		d.Reasons = append(d.Reasons, "local_first=true")
	}
	return d
}

func pickProvider(cfg config.Config) string {
	if cfg.Temper.Preference.LocalFirst {
		if live(cfg, "local-mlx") {
			return "local-mlx"
		}
		if live(cfg, "ollama") {
			return "ollama"
		}
	}
	if _, ok := cfg.Providers["openai"]; ok {
		if cfg.Providers["openai"].Key != "" {
			return "openai"
		}
	}
	if p, ok := cfg.Providers["anthropic"]; ok && p.Key != "" {
		return "anthropic"
	}
	if p, ok := cfg.Providers["gemini"]; ok && p.Key != "" {
		return "gemini"
	}
	for name := range cfg.Providers {
		return name
	}
	return "ollama"
}

func defaultModel(cfg config.Config, provider string) string {
	p, ok := cfg.Providers[provider]
	if !ok {
		return ""
	}
	switch strings.ToLower(p.Type) {
	case "omlx":
		return "local"
	case "ollama":
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

func live(cfg config.Config, name string) bool {
	p, ok := cfg.Providers[name]
	if !ok || p.URL == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(p.URL, "/")+"/", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}
