package run

import (
	"strings"
	"unicode"
)

func continuesGoal(prev, next string) bool {
	prev = normalizeGoal(prev)
	next = normalizeGoal(next)
	if next == "" {
		return true
	}
	if prev == "" || next == prev {
		return true
	}
	if hasPhrase(next, shiftPhrases) {
		return false
	}
	if hasPrefix(next, continuePrefixes) || hasPhrase(next, continuePhrases) {
		return true
	}
	words := strings.Fields(next)
	if len(words) <= 12 {
		return true
	}
	if tokenOverlap(prev, next) >= 0.2 {
		return true
	}
	return false
}

func normalizeGoal(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func tokenOverlap(a, b string) float64 {
	aw := contentTokens(a)
	bw := contentTokens(b)
	if len(aw) == 0 || len(bw) == 0 {
		return 0
	}
	seen := map[string]struct{}{}
	for w := range aw {
		if _, ok := bw[w]; ok {
			seen[w] = struct{}{}
		}
	}
	return float64(len(seen)) / float64(len(bw))
}

func contentTokens(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, w := range strings.Fields(s) {
		if len(w) < 3 {
			continue
		}
		if _, skip := stop[w]; skip {
			continue
		}
		out[w] = struct{}{}
	}
	return out
}

func hasPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func hasPhrase(s string, phrases []string) bool {
	for _, p := range phrases {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

var shiftPhrases = []string{
	"scratch that", "forget that", "forget it", "never mind", "nevermind",
	"new goal", "new task", "different task", "different goal",
	"start over", "start again", "ignore that", "ignore the previous",
	"switch to", "something else", "unrelated",
}

var continuePrefixes = []string{
	"yes", "yeah", "yep", "ok", "okay", "sure", "please", "thanks",
	"also", "and ", "but ", "try ", "use ", "using ", "with ", "via ",
	"don t", "do not", "instead", "actually", "wait",
	"can you", "could you", "just ", "maybe ", "perhaps ",
	"now try", "now use", "now just",
}

var continuePhrases = []string{
	"more context", "additional context", "for example", "e g",
}

var stop = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "that": {}, "this": {}, "with": {},
	"from": {}, "you": {}, "can": {}, "please": {}, "just": {}, "now": {},
}
