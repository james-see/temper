package arbiter

import (
	"fmt"
	"strings"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/provider"
)

// chainOrder returns escalation order: the configured arbiter.escalation
// list first, then any remaining ids in default discovery order.
func chainOrder(cfg config.Config, st provider.Status) []string {
	var order []string
	seen := map[string]bool{}
	for _, id := range cfg.Arbiter.Escalation {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		order = append(order, id)
	}
	for _, id := range provider.PreferredOrder(cfg, st) {
		if seen[id] {
			continue
		}
		seen[id] = true
		order = append(order, id)
	}
	return order
}

// NextInChain returns the next untried, usable, policy-allowed provider in
// escalation order. Callers feed it fresh discovery status plus the run's
// tried set; it is pure over those inputs, so tests never touch the network.
// Reasons narrate every skip and the final pick for explainable routing.
func NextInChain(cfg config.Config, tried map[string]bool, st provider.Status) (provider.Candidate, []string, bool) {
	c := foldPolicies(cfg.Arbiter.Policies)
	order := chainOrder(cfg, st)
	var reasons []string
	var skippedTried []string
	for i, id := range order {
		cand, ok := st.ByID(id)
		if !ok {
			reasons = append(reasons, "chain "+id+": unknown provider")
			continue
		}
		if tried[id] {
			skippedTried = append(skippedTried, id)
			continue
		}
		if !cand.Usable {
			reasons = append(reasons, "chain "+id+": unusable ("+cand.Reason+")")
			continue
		}
		if ok, why := c.providerOK(id); !ok {
			reasons = append(reasons, "chain "+id+": blocked by policy ("+why+")")
			continue
		}
		if len(skippedTried) > 0 {
			reasons = append(reasons, "chain skipped tried: "+strings.Join(skippedTried, ", "))
		}
		reasons = append(reasons, fmt.Sprintf("chain %s selected (position %d/%d)", id, i+1, len(order)))
		return cand, reasons, true
	}
	if len(skippedTried) > 0 {
		reasons = append(reasons, "chain skipped tried: "+strings.Join(skippedTried, ", "))
	}
	reasons = append(reasons, "chain exhausted")
	return provider.Candidate{}, reasons, false
}
