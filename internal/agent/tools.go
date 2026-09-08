package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/james-see/temper/internal/provider"
)

type ToolFunc func(ctx context.Context, args json.RawMessage) (string, []string, error)

type Tool struct {
	Spec provider.ToolSpec
	Run  ToolFunc
}

func NativeTools(root string, timeout time.Duration, deny []string) []Tool {
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return []Tool{
		shellTool(root, timeout, deny),
		readTool(root),
		writeTool(root),
		patchTool(root),
		searchTool(root),
		gitTool(root, timeout),
	}
}

func Specs(tools []Tool) []provider.ToolSpec {
	out := make([]provider.ToolSpec, len(tools))
	for i, t := range tools {
		out[i] = t.Spec
	}
	return out
}

func Lookup(tools []Tool, name string) (Tool, bool) {
	for _, t := range tools {
		if t.Spec.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

func schema(raw string) json.RawMessage { return json.RawMessage(raw) }

func shellTool(root string, timeout time.Duration, deny []string) Tool {
	return Tool{
		Spec: provider.ToolSpec{
			Name:        "shell",
			Description: "Run a shell command in the workspace root.",
			Parameters:  schema(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, []string, error) {
			var in struct{ Command string `json:"command"` }
			if err := json.Unmarshal(args, &in); err != nil {
				return "", nil, err
			}
			for _, d := range deny {
				if d != "" && strings.Contains(in.Command, d) {
					return "", nil, fmt.Errorf("command denied by policy")
				}
			}
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "sh", "-c", in.Command)
			cmd.Dir = root
			var buf bytes.Buffer
			cmd.Stdout = &buf
			cmd.Stderr = &buf
			err := cmd.Run()
			out := buf.String()
			if err != nil {
				return out, nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(out))
			}
			return out, nil, nil
		},
	}
}

func readTool(root string) Tool {
	return Tool{
		Spec: provider.ToolSpec{
			Name:        "read",
			Description: "Read a workspace file.",
			Parameters:  schema(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
		},
		Run: func(_ context.Context, args json.RawMessage) (string, []string, error) {
			var in struct{ Path string `json:"path"` }
			if err := json.Unmarshal(args, &in); err != nil {
				return "", nil, err
			}
			p, err := confined(root, in.Path)
			if err != nil {
				return "", nil, err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return "", nil, err
			}
			if len(b) > 200_000 {
				b = append(b[:200_000], []byte("\n…truncated")...)
			}
			return string(b), nil, nil
		},
	}
}

func writeTool(root string) Tool {
	return Tool{
		Spec: provider.ToolSpec{
			Name:        "write",
			Description: "Write a workspace file. Creates parent dirs.",
			Parameters:  schema(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
		},
		Run: func(_ context.Context, args json.RawMessage) (string, []string, error) {
			var in struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", nil, err
			}
			p, err := confined(root, in.Path)
			if err != nil {
				return "", nil, err
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return "", nil, err
			}
			if err := os.WriteFile(p, []byte(in.Content), 0o644); err != nil {
				return "", nil, err
			}
			rel, _ := filepath.Rel(root, p)
			return "wrote " + rel, []string{rel}, nil
		},
	}
}

func patchTool(root string) Tool {
	return Tool{
		Spec: provider.ToolSpec{
			Name:        "patch",
			Description: "Apply a unified diff with git apply.",
			Parameters:  schema(`{"type":"object","properties":{"diff":{"type":"string"}},"required":["diff"]}`),
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, []string, error) {
			var in struct{ Diff string `json:"diff"` }
			if err := json.Unmarshal(args, &in); err != nil {
				return "", nil, err
			}
			cmd := exec.CommandContext(ctx, "git", "apply", "--whitespace=nowarn", "-")
			cmd.Dir = root
			cmd.Stdin = strings.NewReader(in.Diff)
			out, err := cmd.CombinedOutput()
			if err != nil {
				return string(out), nil, fmt.Errorf("patch: %w: %s", err, strings.TrimSpace(string(out)))
			}
			return "patched", changedFromDiff(in.Diff), nil
		},
	}
}

func searchTool(root string) Tool {
	return Tool{
		Spec: provider.ToolSpec{
			Name:        "search",
			Description: "Search workspace files for a regex/pattern.",
			Parameters:  schema(`{"type":"object","properties":{"pattern":{"type":"string"},"path":{"type":"string"}},"required":["pattern"]}`),
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, []string, error) {
			var in struct {
				Pattern string `json:"pattern"`
				Path    string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", nil, err
			}
			scope := root
			if in.Path != "" {
				p, err := confined(root, in.Path)
				if err != nil {
					return "", nil, err
				}
				scope = p
			}
			cmd := exec.CommandContext(ctx, "rg", "-n", "--hidden", "--glob", "!.git", "--glob", "!.temper", in.Pattern, scope)
			out, err := cmd.CombinedOutput()
			if err == nil {
				return string(out), nil, nil
			}
			cmd = exec.CommandContext(ctx, "grep", "-RIn", "--exclude-dir=.git", "--exclude-dir=.temper", in.Pattern, scope)
			out, err = cmd.CombinedOutput()
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
					return "", nil, nil
				}
				return string(out), nil, err
			}
			return string(out), nil, nil
		},
	}
}

func gitTool(root string, timeout time.Duration) Tool {
	return Tool{
		Spec: provider.ToolSpec{
			Name:        "git",
			Description: "Run a safe git subcommand: status, diff, log, add, commit, rev-parse.",
			Parameters:  schema(`{"type":"object","properties":{"args":{"type":"array","items":{"type":"string"}}},"required":["args"]}`),
		},
		Run: func(ctx context.Context, args json.RawMessage) (string, []string, error) {
			var in struct {
				Args []string `json:"args"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", nil, err
			}
			if len(in.Args) == 0 {
				return "", nil, fmt.Errorf("git args required")
			}
			switch in.Args[0] {
			case "status", "diff", "log", "add", "commit", "rev-parse", "show":
			default:
				return "", nil, fmt.Errorf("git subcommand not allowed: %s", in.Args[0])
			}
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, "git", in.Args...)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			return string(out), nil, err
		},
	}
}

func confined(root, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	abs := p
	if !filepath.IsAbs(p) {
		abs = filepath.Join(root, p)
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes workspace")
	}
	return abs, nil
}

func changedFromDiff(diff string) []string {
	var files []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			files = append(files, strings.TrimPrefix(line, "+++ b/"))
		}
	}
	return files
}
