package arbiter

import (
	"encoding/json"
	"testing"
)

func TestRunTelemetryRoundTrip(t *testing.T) {
	accepted := true
	in := RunTelemetry{
		RunID:            "r1",
		TaskType:         "bugfix",
		Agent:            "native",
		Provider:         "zai",
		Model:            "glm-4.7-flash",
		PromptTokens:     100,
		CompletionTokens: 50,
		LatencyMs:        1200,
		CostUSD:          CostUSD(100, 50),
		TestsPassed:      3,
		TestsFailed:      1,
		ToolErrors:       map[string]int{"shell:exit-1": 2},
		Completed:        true,
		Accepted:         &accepted,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out RunTelemetry
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.RunID != "r1" || out.Provider != "zai" || out.ToolErrors["shell:exit-1"] != 2 {
		t.Fatalf("%+v", out)
	}
	if out.Accepted == nil || !*out.Accepted || out.CostUSD != CostUSD(100, 50) {
		t.Fatalf("%+v", out)
	}
}
