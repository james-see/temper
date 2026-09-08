package provider

import (
	"fmt"
	"strings"

	"github.com/james-see/temper/internal/config"
)

func FromConfig(cfg config.Config, name string) (Provider, error) {
	return Instant(cfg, name)
}

func New(id string, p config.Provider) (Provider, error) {
	switch strings.ToLower(p.Type) {
	case "openai", "openrouter":
		url := p.URL
		if p.Type == "openrouter" && url == "" {
			url = "https://openrouter.ai/api/v1"
		}
		return NewOpenAI(id, url, p.AuthKey()), nil
	case "omlx":
		return NewOMLX(id, p.URL, p.AuthKey()), nil
	case "ollama":
		url := p.URL
		if id == "ollama-cloud" && url == "" {
			url = OllamaCloudURL
		}
		if p.CloudURL != "" && id == "ollama-cloud" {
			url = p.CloudURL
		}
		return NewOllama(id, url, p.AuthKey()), nil
	case "anthropic":
		return NewAnthropic(id, p.URL, p.AuthKey()), nil
	case "gemini":
		return NewGemini(id, p.URL, p.AuthKey()), nil
	default:
		return nil, fmt.Errorf("unsupported provider type %q", p.Type)
	}
}

func Resolve(cfg config.Config, name string) (Provider, string, error) {
	if name != "" {
		p, err := FromConfig(cfg, name)
		return p, name, err
	}
	st := Discover(nil, cfg)
	c, ok := st.Preferred(cfg)
	if !ok {
		return nil, "", fmt.Errorf("no usable providers (missing keys / daemons)")
	}
	p, err := FromConfig(cfg, c.ID)
	return p, c.ID, err
}
