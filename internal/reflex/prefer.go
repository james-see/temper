package reflex

import "strings"

// PreferRecovery returns preferred recovery actions for an assessment, ordered
// most-specific first. Empty means callers should use the default ladder order.
//
// Families / reasons map to interventions that address the failure mode instead
// of always walking replan → critic → switch_model.
func PreferRecovery(a Assessment) []string {
	if strings.HasPrefix(a.Family, "discover:") || a.Family == "discover" {
		// MCP/tool discovery loops need auth/human, not another replan.
		return []string{"human", "critic", "replan"}
	}
	if hasReason(a, "rumination") {
		return []string{"switch_model", "critic", "replan", "human"}
	}
	if hasReason(a, "repeated-error") {
		return []string{"critic", "replan", "switch_model", "human"}
	}
	if hasReason(a, "repeated-action") || hasReason(a, "repeated-cycle") {
		return []string{"replan", "critic", "switch_model", "switch_agent", "human"}
	}
	if hasReason(a, "token-burn") {
		return []string{"switch_model", "replan", "critic", "human"}
	}
	if hasReason(a, "stagnation") || hasReason(a, "semantic-stagnation") {
		return []string{"replan", "switch_model", "critic", "human"}
	}
	if a.State == Regressing || hasReason(a, "evaluator-regression") {
		return []string{"critic", "replan", "human"}
	}
	return nil
}

func hasReason(a Assessment, want string) bool {
	want = strings.ToLower(want)
	for _, r := range a.Reasons {
		if strings.EqualFold(r, want) {
			return true
		}
	}
	if strings.EqualFold(a.Family, want) {
		return true
	}
	return false
}

// NextFor picks the next recovery action: preferred actions for the assessment
// first (when still in the ladder and under MaxEach), then remaining ladder steps.
func (l *Ladder) NextFor(a Assessment) (string, bool) {
	if l == nil {
		return "", false
	}
	inLadder := map[string]bool{}
	for _, s := range l.Steps {
		inLadder[s] = true
	}
	for _, step := range PreferRecovery(a) {
		if !inLadder[step] {
			continue
		}
		if l.Attempts[step] < l.MaxEach {
			l.Attempts[step]++
			return step, true
		}
	}
	return l.Next()
}
