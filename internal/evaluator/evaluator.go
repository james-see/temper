package evaluator

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

type Result struct {
	Kind     string
	Command  string
	Passed   bool
	Output   string
	Duration time.Duration
}

type Engine struct {
	Workdir string
	Test    string
	Compile string
	Timeout time.Duration
}

func (e *Engine) RunAll(ctx context.Context) []Result {
	var out []Result
	if e.Compile != "" {
		out = append(out, e.Run(ctx, "compile", e.Compile))
	}
	if e.Test != "" {
		out = append(out, e.Run(ctx, "test", e.Test))
	}
	return out
}

func (e *Engine) Run(ctx context.Context, kind, command string) Result {
	timeout := e.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = e.Workdir
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return Result{
		Kind:     kind,
		Command:  command,
		Passed:   err == nil,
		Output:   trim(buf.String()),
		Duration: time.Since(start),
	}
}

func AllPassed(rs []Result) bool {
	if len(rs) == 0 {
		return false
	}
	for _, r := range rs {
		if !r.Passed {
			return false
		}
	}
	return true
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 32_000 {
		return s[:32_000] + "\n…truncated"
	}
	return s
}
