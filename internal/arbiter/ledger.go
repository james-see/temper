package arbiter

import (
	"sync"
	"time"
)

// CostPerTokenUSD prices every token (prompt or completion) for budget
// accounting. It is a coarse heuristic until per-model rates land.
const CostPerTokenUSD = 0.000002

// CostUSD prices prompt+completion tokens at the heuristic rate.
func CostUSD(prompt, completion int) float64 {
	return float64(prompt+completion) * CostPerTokenUSD
}

// ProviderStats accumulates cost/latency accounting for one provider.
type ProviderStats struct {
	Calls            int           `json:"calls"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	Errors           int           `json:"errors"`
	Latency          time.Duration `json:"latency_ns"`
}

// CostUSD prices the accumulated tokens at the heuristic rate.
func (s ProviderStats) CostUSD() float64 {
	return CostUSD(s.PromptTokens, s.CompletionTokens)
}

// Ledger accumulates per-provider cost/latency accounting over a run.
type Ledger struct {
	mu         sync.Mutex
	byProvider map[string]*ProviderStats
	order      []string
}

// Observe records one provider call: token usage, latency, and whether the
// call itself failed (tool/eval failures are not provider errors).
func (l *Ledger) Observe(prov string, prompt, completion int, latency time.Duration, err error) {
	if prov == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byProvider == nil {
		l.byProvider = map[string]*ProviderStats{}
	}
	s, ok := l.byProvider[prov]
	if !ok {
		s = &ProviderStats{}
		l.byProvider[prov] = s
		l.order = append(l.order, prov)
	}
	s.Calls++
	s.PromptTokens += prompt
	s.CompletionTokens += completion
	s.Latency += latency
	if err != nil {
		s.Errors++
	}
}

// Summary returns a copy of per-provider stats in first-observed order.
func (l *Ledger) Summary() []NamedStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []NamedStats
	for _, name := range l.order {
		if s, ok := l.byProvider[name]; ok {
			out = append(out, NamedStats{Provider: name, Stats: *s})
		}
	}
	return out
}

// Totals sums every provider's stats.
func (l *Ledger) Totals() ProviderStats {
	l.mu.Lock()
	defer l.mu.Unlock()
	var t ProviderStats
	for _, s := range l.byProvider {
		t.Calls += s.Calls
		t.PromptTokens += s.PromptTokens
		t.CompletionTokens += s.CompletionTokens
		t.Errors += s.Errors
		t.Latency += s.Latency
	}
	return t
}

// NamedStats pairs a provider id with its accumulated stats for events.
type NamedStats struct {
	Provider string        `json:"provider"`
	Stats    ProviderStats `json:"stats"`
}
