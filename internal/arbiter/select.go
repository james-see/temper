package arbiter

import (
	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/provider"
)

func Select(cfg config.Config, agent, prov, model string) Decision {
	explicitProv, explicitModel := prov, model
	if agent == "" && prov == "" && model == "" {
		agent, prov, model = config.Selected(cfg)
	}
	if agent == "" {
		agent = "native"
	}
	st := provider.Discover(nil, cfg)
	reasons := []string{"probe usable providers; ollama-cloud then local ollama"}
	if cfg.Temper.Preference.LocalFirst {
		reasons = append(reasons, "local_first=true")
	}
	if cfg.Temper.Preference.DefaultProvider != "" {
		reasons = append(reasons, "default_provider="+cfg.Temper.Preference.DefaultProvider)
	}

	if c, ok := st.ByID(prov); ok && !c.Usable && explicitProv == "" {
		reasons = append(reasons, "ignoring configured "+prov+": "+c.Reason)
		prov = ""
	}
	if prov == "" {
		if c, ok := st.Preferred(cfg); ok {
			prov = c.ID
			reasons = append(reasons, "preferred="+c.ID+" ("+c.Reason+")")
		}
	} else if c, ok := st.ByID(prov); ok && !c.Usable {
		reasons = append(reasons, "requested "+prov+" is not usable: "+c.Reason)
	}

	if model == "" && prov != "" {
		model = provider.DefaultModel(cfg, prov)
	}
	if explicitModel != "" {
		model = explicitModel
	}

	var consideredOut []Candidate
	for _, c := range st.Usable() {
		consideredOut = append(consideredOut, Candidate{Provider: c.ID, Score: 1})
	}

	return Decision{
		Selected:   Candidate{Agent: agent, Provider: prov, Model: model, Score: 1},
		Considered: consideredOut,
		Reasons:    reasons,
	}
}
