package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Temper    TemperSection            `yaml:"temper"`
	Providers map[string]Provider      `yaml:"providers"`
	Agents    map[string]Agent         `yaml:"agents"`
	Arbiter   Arbiter                  `yaml:"arbiter"`
	Reflex    Reflex                   `yaml:"reflex"`
	Evaluator Evaluator                `yaml:"evaluator"`
	Workspace Workspace                `yaml:"workspace"`
	Shell     Shell                    `yaml:"shell"`
}

type TemperSection struct {
	Budget     Budget     `yaml:"budget"`
	Preference Preference `yaml:"preference"`
}

type Budget struct {
	MaxCostPerTask float64 `yaml:"max_cost_per_task"`
}

type Preference struct {
	LocalFirst bool `yaml:"local_first"`
}

type Provider struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url,omitempty"`
	Key  string `yaml:"key,omitempty"`
}

type Agent struct {
	Type string `yaml:"type"`
}

type Arbiter struct {
	Policies []Policy `yaml:"policies"`
}

type Policy struct {
	Match  map[string]string `yaml:"match"`
	Prefer PolicyPrefer      `yaml:"prefer"`
}

type PolicyPrefer struct {
	Agent    string `yaml:"agent"`
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

type Reflex struct {
	Detectors ReflexDetectors `yaml:"detectors"`
	Recovery  []RecoveryStep  `yaml:"recovery"`
	Judge     Judge           `yaml:"judge"`
}

type ReflexDetectors struct {
	RepeatedError DetectorThresh `yaml:"repeated_error"`
	ActionCycle   DetectorRep    `yaml:"action_cycle"`
	Stagnation    DetectorActions `yaml:"stagnation"`
	TokenBurn     DetectorThresh `yaml:"token_burn"`
	Regression    DetectorFlag   `yaml:"regression"`
}

type DetectorThresh struct {
	Threshold int `yaml:"threshold"`
}

type DetectorRep struct {
	Repetitions int `yaml:"repetitions"`
}

type DetectorActions struct {
	Actions int `yaml:"actions"`
}

type DetectorFlag struct {
	Enabled bool `yaml:"enabled"`
}

type RecoveryStep struct {
	After  string `yaml:"after"`
	Action string `yaml:"action"`
}

type Judge struct {
	Model     string `yaml:"model"`
	Endpoint  string `yaml:"endpoint"`
	AutoPull  bool   `yaml:"auto_pull"`
}

type Evaluator struct {
	Test    string `yaml:"test"`
	Compile string `yaml:"compile"`
}

type Workspace struct {
	Root string `yaml:"root"`
}

type Shell struct {
	Timeout time.Duration `yaml:"timeout"`
	Deny    []string      `yaml:"deny"`
}

type Flags struct {
	Plain    bool
	Config   string
	Agent    string
	Provider string
	Model    string
}

type Loaded struct {
	Config  Config
	Sources map[string]string
	Files   []string
}

func Defaults() Config {
	return Config{
		Temper: TemperSection{
			Budget:     Budget{MaxCostPerTask: 5.00},
			Preference: Preference{LocalFirst: true},
		},
		Providers: map[string]Provider{
			"local-mlx": {Type: "omlx", URL: "http://localhost:8000"},
			"ollama":    {Type: "ollama", URL: "http://localhost:11434"},
			"anthropic": {Type: "anthropic"},
			"openai":    {Type: "openai"},
			"gemini":    {Type: "gemini"},
			"openrouter": {Type: "openrouter"},
		},
		Agents: map[string]Agent{
			"native": {Type: "temper"},
		},
		Reflex: Reflex{
			Detectors: ReflexDetectors{
				RepeatedError: DetectorThresh{Threshold: 3},
				ActionCycle:   DetectorRep{Repetitions: 2},
				Stagnation:    DetectorActions{Actions: 6},
				TokenBurn:     DetectorThresh{Threshold: 20000},
				Regression:    DetectorFlag{Enabled: true},
			},
			Recovery: []RecoveryStep{
				{After: "first_stall", Action: "replan"},
				{After: "second_stall", Action: "critic"},
				{After: "third_stall", Action: "switch_model"},
				{After: "fourth_stall", Action: "switch_agent"},
				{After: "exhausted", Action: "human"},
			},
			Judge: Judge{
				Model: "LiquidAI/LFM2.5-2.6B-GGUF:Q4_K_M",
			},
		},
		Evaluator: Evaluator{},
		Workspace: Workspace{Root: "."},
		Shell:     Shell{Timeout: 60 * time.Second},
	}
}

func Load(flags Flags) (*Loaded, error) {
	cfg := Defaults()
	sources := flattenSources(cfg, "default")
	var files []string

	for _, p := range userPaths() {
		if err := mergeFile(&cfg, sources, p); err == nil {
			files = append(files, p)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("load %s: %w", p, err)
		}
	}
	for _, p := range projectPaths() {
		if err := mergeFile(&cfg, sources, p); err == nil {
			files = append(files, p)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("load %s: %w", p, err)
		}
	}
	if flags.Config != "" {
		if err := mergeFile(&cfg, sources, flags.Config); err != nil {
			return nil, fmt.Errorf("load %s: %w", flags.Config, err)
		}
		files = append(files, flags.Config)
	}

	applyEnv(&cfg, sources)
	applyFlags(&cfg, flags, sources)

	if cfg.Workspace.Root == "" {
		cfg.Workspace.Root = "."
	}
	if cfg.Shell.Timeout == 0 {
		cfg.Shell.Timeout = 60 * time.Second
	}
	return &Loaded{Config: cfg, Sources: sources, Files: files}, nil
}

func userPaths() []string {
	home, _ := os.UserHomeDir()
	var out []string
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		out = append(out, filepath.Join(xdg, "temper", "temper.yaml"))
	} else if home != "" {
		out = append(out, filepath.Join(home, ".config", "temper", "temper.yaml"))
	}
	if home != "" {
		out = append(out, filepath.Join(home, ".temper.yaml"))
	}
	return out
}

func projectPaths() []string {
	if _, err := os.Stat("temper.yaml"); err == nil {
		return []string{"temper.yaml"}
	}
	if _, err := os.Stat(".temper/config.yaml"); err == nil {
		return []string{".temper/config.yaml"}
	}
	return nil
}

func mergeFile(cfg *Config, sources map[string]string, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var overlay map[string]any
	if err := yaml.Unmarshal(raw, &overlay); err != nil {
		return err
	}
	base := toMap(*cfg)
	deepMerge(base, overlay)
	merged, err := yaml.Marshal(base)
	if err != nil {
		return err
	}
	var next Config
	if err := yaml.Unmarshal(merged, &next); err != nil {
		return err
	}
	*cfg = next
	markSources(overlay, "", path, sources)
	return nil
}

func applyEnv(cfg *Config, sources map[string]string) {
	if v := os.Getenv("TEMPER_AGENT"); v != "" {
		if cfg.Arbiter.Policies == nil {
			cfg.Arbiter.Policies = nil
		}
		sources["flags.agent"] = "env:TEMPER_AGENT"
		setSelected(cfg, "agent", v)
	}
	if v := os.Getenv("TEMPER_PROVIDER"); v != "" {
		sources["flags.provider"] = "env:TEMPER_PROVIDER"
		setSelected(cfg, "provider", v)
	}
	if v := os.Getenv("TEMPER_MODEL"); v != "" {
		sources["flags.model"] = "env:TEMPER_MODEL"
		setSelected(cfg, "model", v)
	}
	if v := os.Getenv("TEMPER_BUDGET"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Temper.Budget.MaxCostPerTask = f
			sources["temper.budget.max_cost_per_task"] = "env:TEMPER_BUDGET"
		}
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		p := cfg.Providers["openai"]
		p.Type = first(p.Type, "openai")
		p.Key = v
		if cfg.Providers == nil {
			cfg.Providers = map[string]Provider{}
		}
		cfg.Providers["openai"] = p
		sources["providers.openai.key"] = "env:OPENAI_API_KEY"
	}
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		p := cfg.Providers["anthropic"]
		p.Type = first(p.Type, "anthropic")
		p.Key = v
		if cfg.Providers == nil {
			cfg.Providers = map[string]Provider{}
		}
		cfg.Providers["anthropic"] = p
		sources["providers.anthropic.key"] = "env:ANTHROPIC_API_KEY"
	}
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
		p := cfg.Providers["gemini"]
		p.Type = first(p.Type, "gemini")
		p.Key = v
		if cfg.Providers == nil {
			cfg.Providers = map[string]Provider{}
		}
		cfg.Providers["gemini"] = p
		sources["providers.gemini.key"] = "env:GEMINI_API_KEY"
	}
	if v := os.Getenv("TEMPER_JUDGE_MODEL"); v != "" {
		cfg.Reflex.Judge.Model = v
		sources["reflex.judge.model"] = "env:TEMPER_JUDGE_MODEL"
	}
	if v := os.Getenv("TEMPER_JUDGE_ENDPOINT"); v != "" {
		cfg.Reflex.Judge.Endpoint = v
		sources["reflex.judge.endpoint"] = "env:TEMPER_JUDGE_ENDPOINT"
	}
}

func applyFlags(cfg *Config, flags Flags, sources map[string]string) {
	if flags.Agent != "" {
		setSelected(cfg, "agent", flags.Agent)
		sources["flags.agent"] = "flag:--agent"
	}
	if flags.Provider != "" {
		setSelected(cfg, "provider", flags.Provider)
		sources["flags.provider"] = "flag:--provider"
	}
	if flags.Model != "" {
		setSelected(cfg, "model", flags.Model)
		sources["flags.model"] = "flag:--model"
	}
	if flags.Plain {
		sources["flags.plain"] = "flag:--plain"
	}
}

func setSelected(cfg *Config, key, value string) {
	if len(cfg.Arbiter.Policies) == 0 {
		cfg.Arbiter.Policies = []Policy{{Prefer: PolicyPrefer{}}}
	}
	p := cfg.Arbiter.Policies[0]
	switch key {
	case "agent":
		p.Prefer.Agent = value
	case "provider":
		p.Prefer.Provider = value
	case "model":
		p.Prefer.Model = value
	}
	cfg.Arbiter.Policies[0] = p
}

func Selected(cfg Config) (agent, provider, model string) {
	if len(cfg.Arbiter.Policies) > 0 {
		p := cfg.Arbiter.Policies[0].Prefer
		return p.Agent, p.Provider, p.Model
	}
	return "", "", ""
}

func (c Config) YAML() (string, error) {
	b, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func flattenSources(cfg Config, src string) map[string]string {
	out := map[string]string{}
	markSources(toMap(cfg), "", src, out)
	return out
}

func markSources(v any, prefix, src string, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 && prefix != "" {
			out[prefix] = src
			return
		}
		for k, child := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			markSources(child, p, src, out)
		}
	default:
		if prefix != "" {
			out[prefix] = src
		}
	}
}

func toMap(cfg Config) map[string]any {
	b, _ := yaml.Marshal(cfg)
	var m map[string]any
	_ = yaml.Unmarshal(b, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

func deepMerge(dst, src map[string]any) {
	for k, sv := range src {
		if sm, ok := sv.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				deepMerge(dm, sm)
				continue
			}
		}
		dst[k] = sv
	}
}

func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func DataDir(root string) string {
	return filepath.Join(root, ".temper")
}

func (c Config) WorkspaceRoot() (string, error) {
	root := c.Workspace.Root
	if root == "" {
		root = "."
	}
	return filepath.Abs(root)
}

func Redact(cfg Config) Config {
	out := cfg
	out.Providers = map[string]Provider{}
	for k, p := range cfg.Providers {
		if p.Key != "" {
			p.Key = strings.Repeat("*", 8)
		}
		out.Providers[k] = p
	}
	return out
}
