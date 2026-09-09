package reflex

import (
	"strings"
	"unicode"
)

var thinkContinue = []string{
	"keep searching",
	"search again",
	"search repeatedly",
	"keep repeating",
	"repeat the same",
	"same search",
	"same query",
	"ignore the warning",
	"ignoring the warning",
	"despite the warning",
	"despite knowing",
	"until temper",
	"i will search",
	"i'll search again",
	"let me search again",
	"let me search",
	"let me try to call",
	"try to call",
	"try calling",
	"try the same",
	"not available in this session",
	"tools aren't",
	"tools are not",
	"not being discovered",
	"call it directly",
	"correct name",
	"different name",
	"not loaded",
}

var thinkStop = []string{
	"already stopped",
	"i stopped",
	"i have already stopped",
	"waiting for direction",
	"waiting for",
	"should i",
	"would you like",
	"ask the user",
	"unproductive",
	"stop here",
	"not looping",
	"i'm not looping",
	"reported the result",
}

func (e *Engine) ObserveThink(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	e.thinks = append(e.thinks, text)
}

func thinkContent(prev []string, current string) (Assessment, bool) {
	current = strings.TrimSpace(current)
	if current == "" {
		return Assessment{}, false
	}
	low := strings.ToLower(current)
	cont := countPhrases(low, thinkContinue)
	stop := countPhrases(low, thinkStop)
	if thinkSimilar(prev, current) {
		cont++
	}
	if cont >= 2 && cont > stop {
		return Assessment{State: Looping, Score: 0.15, Reasons: []string{"think-loop"}}, true
	}
	if cont >= 1 && stop == 0 {
		return Assessment{State: Uncertain, Score: 0.3, Reasons: []string{"think-risk"}}, true
	}
	return Assessment{}, false
}

func countPhrases(low string, phrases []string) int {
	n := 0
	for _, p := range phrases {
		if strings.Contains(low, p) {
			n++
		}
	}
	return n
}

func thinkSimilar(prev []string, current string) bool {
	if len(prev) == 0 {
		return false
	}
	a := thinkWords(prev[len(prev)-1])
	b := thinkWords(current)
	if len(a) < 12 || len(b) < 12 {
		return false
	}
	return jaccard(a, b) >= 0.65
}

func thinkWords(s string) map[string]struct{} {
	out := map[string]struct{}{}
	var b strings.Builder
	flush := func() {
		w := strings.ToLower(b.String())
		b.Reset()
		if len(w) < 4 {
			return
		}
		out[w] = struct{}{}
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if _, ok := b[w]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
