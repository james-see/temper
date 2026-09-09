package run

import (
	"strings"
	"unicode"
)

const (
	turnAck      = "ack"
	turnFollowup = "follow-up"
	turnModify   = "modify"
	turnNew      = "new"
)

func continuesGoal(prev, next string) bool {
	return classifyTurn(prev, next) != turnNew
}

func classifyTurn(prev, next string) string {
	prev = normalizeGoal(prev)
	next = normalizeGoal(next)
	if next == "" {
		return turnAck
	}
	if prev == "" {
		return turnNew
	}
	if next == prev {
		return turnAck
	}
	if hasPhrase(next, shiftPhrases) {
		return turnNew
	}
	if isAck(next) {
		return turnAck
	}
	if hasPrefix(next, modifyPrefixes) || hasPhrase(next, modifyPhrases) {
		return turnModify
	}
	if hasPrefix(next, continuePrefixes) || hasPhrase(next, continuePhrases) || hasPhrase(next, followupMarkers) {
		return turnFollowup
	}
	overlap := tokenOverlap(prev, next)
	if overlap >= 0.25 {
		return turnFollowup
	}
	if looksStandaloneTask(next) {
		return turnNew
	}
	words := strings.Fields(next)
	if len(words) <= 4 {
		return turnFollowup
	}
	if overlap < 0.15 {
		return turnNew
	}
	return turnFollowup
}

func isAck(s string) bool {
	return ackExact[s]
}

func looksStandaloneTask(s string) bool {
	words := strings.Fields(s)
	if len(words) == 0 {
		return false
	}
	if taskStart[words[0]] {
		return true
	}
	for _, w := range words {
		if taskStrong[w] {
			return true
		}
	}
	return hasPrefix(s, askPrefixes) && len(words) >= 4
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

var modifyPrefixes = []string{
	"instead", "actually", "wait", "don t", "do not",
}

var modifyPhrases = []string{
	"different approach", "another way", "another approach",
}

var continuePrefixes = []string{
	"yes", "yeah", "yep", "ok", "okay", "sure", "thanks",
	"also", "and ", "but ", "try ", "use ", "using ", "with ", "via ",
	"just ", "maybe ", "perhaps ",
	"now try", "now use", "now just",
}

var continuePhrases = []string{
	"more context", "additional context", "for example", "e g",
}

var followupMarkers = []string{
	" too", "as well", "again", "the error", "that error",
	"same thing", "why so", "how come", "that one", "this one",
	"the same",
}

var askPrefixes = []string{
	"can we", "can you", "could you", "could we", "would you",
	"what ", "who ", "when ", "where ", "why ", "how ",
	"is there", "are there",
}

var taskStart = map[string]bool{
	"run": true, "implement": true, "write": true, "create": true,
	"install": true, "search": true, "refactor": true, "migrate": true,
	"deploy": true, "analyze": true, "compare": true, "fetch": true,
	"download": true, "configure": true, "document": true, "explain": true,
}

var taskStrong = map[string]bool{
	"implement": true, "refactor": true, "migrate": true, "deploy": true,
	"speedtest": true,
}

var ackExact = map[string]bool{
	"yes": true, "yeah": true, "yep": true, "yup": true,
	"ok": true, "okay": true, "sure": true, "thanks": true,
	"thank you": true, "please": true, "go ahead": true,
	"continue": true, "lgtm": true, "do it": true,
}

var stop = map[string]struct{}{
	"the": {}, "and": {}, "for": {}, "that": {}, "this": {}, "with": {},
	"from": {}, "you": {}, "can": {}, "please": {}, "just": {}, "now": {},
}
