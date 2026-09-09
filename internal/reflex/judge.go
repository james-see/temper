package reflex

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/james-see/temper/internal/provider"
)

var pulled sync.Map

type JudgeInput struct {
	Events   string
	Tokens   int
	Cost     float64
	EvalNote string
}

type JudgeResult struct {
	Assessment Assessment
	Usage      provider.Usage
	Used       bool
	Err        error
}

func InvokeJudge(ctx context.Context, endpoint, model string, in JudgeInput) JudgeResult {
	prompt := fmt.Sprintf(`Classify this coding-agent run. Return JSON only: {"state":"progressing|uncertain|stalled|looping|regressing|complete","score":0-1,"reasons":["..."]}
state must be one of those six values.
Tokens=%d Cost=%.4f Eval=%s
Events:
%s`, in.Tokens, in.Cost, in.EvalNote, trim(in.Events, 4000))

	if endpoint == "" || strings.Contains(endpoint, "11434") || looksOllama(endpoint) {
		o := provider.NewOllama("judge-ollama", firstURL(endpoint, "http://localhost:11434"), "")
		content, usage, err := o.ChatJSON(ctx, model, prompt, 150)
		if err == nil {
			a, perr := parseAssessment(content)
			return JudgeResult{Assessment: a, Usage: usage, Used: true, Err: perr}
		}
		if endpoint != "" && looksOllama(endpoint) {
			return JudgeResult{Err: err}
		}
	}

	url := firstURL(endpoint, "http://localhost:8080/v1")
	if !strings.HasSuffix(url, "/v1") && !strings.Contains(url, "/chat/completions") {
		url = strings.TrimRight(url, "/") + "/v1"
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 150,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(url, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return JudgeResult{Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return JudgeResult{Err: err}
	}
	defer resp.Body.Close()
	var decoded struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return JudgeResult{Err: err}
	}
	if resp.StatusCode >= 300 {
		msg := fmt.Sprintf("judge: %d", resp.StatusCode)
		if decoded.Error != nil {
			msg += " " + decoded.Error.Message
		}
		return JudgeResult{Err: fmt.Errorf("%s", msg)}
	}
	content := ""
	if len(decoded.Choices) > 0 {
		content = decoded.Choices[0].Message.Content
	}
	a, perr := parseAssessment(content)
	return JudgeResult{
		Assessment: a,
		Usage:      provider.Usage{PromptTokens: decoded.Usage.PromptTokens, CompletionTokens: decoded.Usage.CompletionTokens},
		Used:       true,
		Err:        perr,
	}
}

func parseAssessment(s string) (Assessment, error) {
	s = extractJSON(s)
	var raw struct {
		State   string   `json:"state"`
		Score   float64  `json:"score"`
		Reasons []string `json:"reasons"`
	}
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return Assessment{State: Uncertain, Score: 0.4, Reasons: []string{"judge-parse-failed"}}, err
	}
	st := ProgressState(strings.ToLower(raw.State))
	switch st {
	case Progressing, Uncertain, Stalled, Looping, Regressing, Complete:
	default:
		st = Uncertain
	}
	return Assessment{State: st, Score: raw.Score, Reasons: raw.Reasons}, nil
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			return s[i : j+1]
		}
	}
	return s
}

func EnsureOllamaModel(ctx context.Context, endpoint, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	if _, ok := pulled.Load(model); ok {
		return nil
	}
	base := firstURL(endpoint, "http://localhost:11434")
	if strings.HasSuffix(base, "/v1") {
		base = strings.TrimSuffix(base, "/v1")
	}
	body, _ := json.Marshal(map[string]any{"name": model, "stream": false})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/api/pull", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("ollama pull %s: %d", model, resp.StatusCode)
	}
	pulled.Store(model, true)
	return nil
}

func looksOllama(endpoint string) bool {
	return strings.Contains(endpoint, "11434") || strings.Contains(endpoint, "ollama")
}

func firstURL(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
