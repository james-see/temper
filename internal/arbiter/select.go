package arbiter

import (
	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/provider"
)

// Select routes agent/provider/model over live discovery status.
func Select(cfg config.Config, agent, prov, model string) Decision {
	return SelectWithStatus(cfg, agent, prov, model, provider.Discover(nil, cfg))
}

// SelectWithStatus is the pure routing core: deterministic over the given
// discovery status, so tests never touch the network. Precedence: hard
// policy deny > policy allow > explicit selections > local-first > default
// discovery order. Every override is narrated in Reasons.
func SelectWithStatus(cfg config.Config, agent, prov, model string, st provider.Status) Decision {
	c := foldPolicies(cfg.Arbiter.Policies)
	explicitProv, explicitModel := prov, model
	if agent == "" && prov == "" && model == "" {
		agent, prov, model = config.Selected(cfg)
	}
	if agent == "" {
		agent = "native"
	}
	reasons := []string{"probe usable providers; ollama-cloud then local ollama"}
	if cfg.Temper.Preference.LocalFirst {
		reasons = append(reasons, "local_first=true")
	}
	if cfg.Temper.Preference.DefaultProvider != "" {
		reasons = append(reasons, "default_provider="+cfg.Temper.Preference.DefaultProvider)
	}

	if ok, why := c.agentOK(agent); !ok {
		reasons = append(reasons, "agent "+agent+" blocked by policy ("+why+")")
		if nativeOK, _ := c.agentOK("native"); agent != "native" && nativeOK {
			agent = "native"
			reasons = append(reasons, "agent fallback to native")
		} else {
			reasons = append(reasons, "policy denies every agent; proceeding with "+agent)
		}
	}

	if cc, ok := st.ByID(prov); ok && !cc.Usable && explicitProv == "" {
		reasons = append(reasons, "ignoring configured "+prov+": "+cc.Reason)
		prov = ""
	}
	if prov != "" {
		if ok, why := c.providerOK(prov); !ok {
			note := "ignoring configured " + prov + ": blocked by policy (" + why + ")"
			if explicitProv != "" {
				note = "explicit provider " + prov + " blocked by policy (" + why + ")"
			}
			reasons = append(reasons, note)
			prov = ""
		}
	}
	if prov == "" {
		if cc, ok := st.Preferred(cfg); ok {
			if okPol, why := c.providerOK(cc.ID); okPol {
				prov = cc.ID
				reasons = append(reasons, "preferred="+cc.ID+" ("+cc.Reason+")")
				if cfg.Temper.Preference.LocalFirst && explicitProv == "" && !isLocalProvider(cc.ID) {
					if local, ok := firstUsableLocal(cfg, c, st); ok {
						reasons = append(reasons, "local_first enforced: "+local.ID+" over "+cc.ID)
						prov = local.ID
					}
				}
			} else {
				reasons = append(reasons, "preferred "+cc.ID+" blocked by policy ("+why+")")
				if next, ok := firstAllowedUsable(cfg, c, st); ok {
					prov = next.ID
					reasons = append(reasons, "policy fallback="+next.ID+" ("+next.Reason+")")
				}
			}
		} else if next, ok := firstAllowedUsable(cfg, c, st); ok {
			prov = next.ID
			reasons = append(reasons, "policy fallback="+next.ID+" ("+next.Reason+")")
		}
	} else if cc, ok := st.ByID(prov); ok && !cc.Usable {
		reasons = append(reasons, "requested "+prov+" is not usable: "+cc.Reason)
	}

	if model == "" && prov != "" {
		model = provider.DefaultModel(cfg, prov)
	}
	if explicitModel != "" {
		model = explicitModel
	}

	var consideredOut []Candidate
	for _, cc := range st.Usable() {
		consideredOut = append(consideredOut, Candidate{Provider: cc.ID, Score: 1})
	}

	return Decision{
		Selected:   Candidate{Agent: agent, Provider: prov, Model: model, Score: 1},
		Considered: consideredOut,
		Reasons:    reasons,
	}
}

// firstAllowedUsable scans default discovery order for the first usable,
// policy-allowed provider.
func firstAllowedUsable(cfg config.Config, c constraints, st provider.Status) (provider.Candidate, bool) {
	for _, id := range provider.PreferredOrder(cfg, st) {
		cc, ok := st.ByID(id)
		if !ok || !cc.Usable {
			continue
		}
		if ok, _ := c.providerOK(cc.ID); !ok {
			continue
		}
		return cc, true
	}
	return provider.Candidate{}, false
}

// firstUsableLocal returns the first usable, policy-allowed local provider
// in default discovery order.
func firstUsableLocal(cfg config.Config, c constraints, st provider.Status) (provider.Candidate, bool) {
	for _, id := range provider.PreferredOrder(cfg, st) {
		cc, ok := st.ByID(id)
		if !ok || !cc.Usable || !isLocalProvider(cc.ID) {
			continue
		}
		if ok, _ := c.providerOK(cc.ID); !ok {
			continue
		}
		return cc, true
	}
	return provider.Candidate{}, false
}
