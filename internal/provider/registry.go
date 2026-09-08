package provider

import (
	"fmt"
	"strings"

	"github.com/james-see/temper/internal/config"
)

func FromConfig(cfg config.Config, name string) (Provider, error) {
	p, ok := cfg.Providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", name)
	}
	return New(name, p)
}

func New(id string, p config.Provider) (Provider, error) {
	switch strings.ToLower(p.Type) {
	case "openai", "openrouter":
		url := p.URL
		if p.Type == "openrouter" && url == "" {
			url = "https://openrouter.ai/api/v1"
		}
		return NewOpenAI(id, url, p.Key), nil
	case "omlx":
		return NewOMLX(id, p.URL, p.Key), nil
	case "ollama":
		return NewOllama(id, p.URL), nil
	case "anthropic":
		return NewAnthropic(id, p.URL, p.Key), nil
	case "gemini":
		return NewGemini(id, p.URL, p.Key), nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q", p.Type)
	}
}

func Resolve(cfg config.Config, name string) (Provider, string, error) {
	if name != "" {
		p, err := FromConfig(cfg, name)
		return p, name, err
	}
	order := []string{}
	if cfg.Temper.Preference.LocalFirst {
		order = append(order, "local-mlx", "ollama")
	}
	for k := range cfg.Providers {
		order = append(order, k)
	}
	seen := map[string]bool{}
	var last error
	for _, k := range order {
		if seen[k] {
			continue
		}
		seen[k] = true
		p, err := FromConfig(cfg, k)
		if err != nil {
			last = err
			continue
		}
		return p, k, nil
	}
	if last != nil {
		return nil, "", last
	}
	return nil, "", fmt.Errorf("no providers configured")
}
