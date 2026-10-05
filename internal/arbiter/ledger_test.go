package arbiter

import (
	"errors"
	"testing"
	"time"
)

func TestLedgerAccumulatesPerProvider(t *testing.T) {
	var l Ledger
	l.Observe("ollama", 10, 5, 100*time.Millisecond, nil)
	l.Observe("openai", 20, 10, 200*time.Millisecond, errors.New("boom"))
	l.Observe("ollama", 10, 5, 100*time.Millisecond, nil)
	l.Observe("", 99, 99, time.Second, nil) // untracked provider ignored

	sum := l.Summary()
	if len(sum) != 2 || sum[0].Provider != "ollama" || sum[1].Provider != "openai" {
		t.Fatalf("order %+v", sum)
	}
	if sum[0].Stats.Calls != 2 || sum[0].Stats.PromptTokens != 20 || sum[0].Stats.CompletionTokens != 10 {
		t.Fatalf("ollama %+v", sum[0].Stats)
	}
	if sum[0].Stats.Latency != 200*time.Millisecond {
		t.Fatalf("latency %v", sum[0].Stats.Latency)
	}
	if sum[1].Stats.Errors != 1 {
		t.Fatalf("openai %+v", sum[1].Stats)
	}
	if got := sum[0].Stats.CostUSD(); got != CostUSD(20, 10) {
		t.Fatalf("cost %v", got)
	}
	tot := l.Totals()
	if tot.Calls != 3 || tot.PromptTokens != 40 || tot.CompletionTokens != 20 || tot.Errors != 1 {
		t.Fatalf("totals %+v", tot)
	}
	if got := CostUSD(1_000_000, 0); got != 2.0 {
		t.Fatalf("rate %v", got)
	}
}
