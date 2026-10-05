package arbiter

import (
	"github.com/james-see/temper/internal/config"
)

// isLocalProvider reports whether id executes on this machine. It mirrors
// the local-first ranking in provider.PreferredOrder.
func isLocalProvider(id string) bool {
	return id == "ollama" || id == "local-mlx"
}

// constraints folds every policy's hard allow/deny lists into effective
// sets. Denies union (any deny blocks); allows union as well (a candidate
// passes when no policy lists allows, or when at least one allow list names
// it). Policy Match is reserved for future conditional policies and is
// currently ignored: constraints apply globally.
type constraints struct {
	allowProv, denyProv   map[string]bool
	allowAgent, denyAgent map[string]bool
	hasAllowProv          bool
	hasAllowAgent         bool
}

func foldPolicies(ps []config.Policy) constraints {
	c := constraints{
		allowProv: map[string]bool{}, denyProv: map[string]bool{},
		allowAgent: map[string]bool{}, denyAgent: map[string]bool{},
	}
	for _, p := range ps {
		for _, id := range p.AllowProviders {
			if id != "" {
				c.allowProv[id] = true
				c.hasAllowProv = true
			}
		}
		for _, id := range p.DenyProviders {
			if id != "" {
				c.denyProv[id] = true
			}
		}
		for _, id := range p.AllowAgents {
			if id != "" {
				c.allowAgent[id] = true
				c.hasAllowAgent = true
			}
		}
		for _, id := range p.DenyAgents {
			if id != "" {
				c.denyAgent[id] = true
			}
		}
	}
	return c
}

// providerOK enforces hard constraints: deny wins over allow, and both win
// over preferences and explicit selections.
func (c constraints) providerOK(id string) (bool, string) {
	if c.denyProv[id] {
		return false, "denied by policy"
	}
	if c.hasAllowProv && !c.allowProv[id] {
		return false, "not in policy allow list"
	}
	return true, ""
}

// agentOK enforces hard agent constraints with the same precedence.
func (c constraints) agentOK(id string) (bool, string) {
	if c.denyAgent[id] {
		return false, "denied by policy"
	}
	if c.hasAllowAgent && !c.allowAgent[id] {
		return false, "not in policy allow list"
	}
	return true, ""
}
