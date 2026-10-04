package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// GitHub implements Forge through the gh CLI (no new dependencies).
// All commands run with cwd set to the repo dir so auth and repo context
// resolve exactly as they do for the operator's own gh invocations.
type GitHub struct {
	LookPath func(string) (string, error)
	Exec     func(ctx context.Context, dir, name string, args []string) (string, error)
}

// NewGitHub builds a GitHub forge backed by the real gh binary.
func NewGitHub() *GitHub {
	return &GitHub{LookPath: exec.LookPath, Exec: runInDir}
}

func runInDir(ctx context.Context, dir, name string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return "", fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return stdout.String(), nil
}

func (g *GitHub) lookPath() func(string) (string, error) {
	if g.LookPath != nil {
		return g.LookPath
	}
	return exec.LookPath
}

func (g *GitHub) exec() func(ctx context.Context, dir, name string, args []string) (string, error) {
	if g.Exec != nil {
		return g.Exec
	}
	return runInDir
}

func (g *GitHub) bin() (string, error) {
	bin, err := g.lookPath()("gh")
	if err != nil {
		return "", fmt.Errorf("gh not on PATH: %w", err)
	}
	return bin, nil
}

const prJSONFields = "number,url,title,state,headRefName,baseRefName,isDraft"

// Create opens a PR from the repo at dir. Head defaults to the current
// branch when empty; base is required.
func (g *GitHub) Create(ctx context.Context, dir string, opt CreateOptions) (PR, error) {
	bin, err := g.bin()
	if err != nil {
		return PR{}, err
	}
	if strings.TrimSpace(opt.Title) == "" {
		return PR{}, fmt.Errorf("pr title required")
	}
	args := []string{"pr", "create", "--title", opt.Title, "--json", prJSONFields}
	if strings.TrimSpace(opt.Body) != "" {
		args = append(args, "--body", opt.Body)
	}
	if strings.TrimSpace(opt.Base) != "" {
		args = append(args, "--base", opt.Base)
	}
	if strings.TrimSpace(opt.Head) != "" {
		args = append(args, "--head", opt.Head)
	}
	if opt.Draft {
		args = append(args, "--draft")
	}
	out, err := g.exec()(ctx, dir, bin, args)
	if err != nil {
		return PR{}, err
	}
	return parsePRJSON(out)
}

// Find detects the open PR for branch. It reports found=false (no error)
// when the branch has no open PR.
func (g *GitHub) Find(ctx context.Context, dir, branch string) (PR, bool, error) {
	bin, err := g.bin()
	if err != nil {
		return PR{}, false, err
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return PR{}, false, fmt.Errorf("branch required")
	}
	out, err := g.exec()(ctx, dir, bin, []string{
		"pr", "list", "--head", branch, "--state", "open",
		"--json", prJSONFields, "--limit", "1",
	})
	if err != nil {
		return PR{}, false, err
	}
	prs, err := parsePRListJSON(out)
	if err != nil {
		return PR{}, false, err
	}
	if len(prs) == 0 {
		return PR{}, false, nil
	}
	return prs[0], true, nil
}

// Sync refreshes PR state and CI checks by number.
func (g *GitHub) Sync(ctx context.Context, dir string, number int) (PR, error) {
	if number <= 0 {
		return PR{}, errNoNumber
	}
	bin, err := g.bin()
	if err != nil {
		return PR{}, err
	}
	out, err := g.exec()(ctx, dir, bin, []string{
		"pr", "view", fmt.Sprint(number),
		"--json", prJSONFields + ",statusCheckRollup",
	})
	if err != nil {
		return PR{}, err
	}
	return parsePRJSON(out)
}

// Checks ingests CI checks for ref, which may name a PR (number, branch, or
// URL). When gh reports no PR for ref and ref looks like a commit SHA, it
// falls back to workflow runs for that commit.
func (g *GitHub) Checks(ctx context.Context, dir, ref string) ([]Check, error) {
	bin, err := g.bin()
	if err != nil {
		return nil, err
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("ref required")
	}
	out, err := g.exec()(ctx, dir, bin, []string{
		"pr", "checks", ref, "--json", "name,state,conclusion,link",
	})
	if err == nil {
		return parseChecksJSON(out)
	}
	if !looksLikeSHA(ref) {
		return nil, err
	}
	out, rerr := g.exec()(ctx, dir, bin, []string{
		"run", "list", "--commit", ref, "--limit", "20",
		"--json", "name,status,conclusion,url",
	})
	if rerr != nil {
		return nil, err
	}
	return parseRunsJSON(out)
}

func looksLikeSHA(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

type ghPR struct {
	Number    int           `json:"number"`
	URL       string        `json:"url"`
	Title     string        `json:"title"`
	State     string        `json:"state"`
	HeadRef   string        `json:"headRefName"`
	BaseRef   string        `json:"baseRefName"`
	IsDraft   bool          `json:"isDraft"`
	CheckRoll []ghCheckRoll `json:"statusCheckRollup"`
}

type ghCheckRoll struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"detailsUrl"`
}

func parsePRJSON(out string) (PR, error) {
	var raw ghPR
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return PR{}, fmt.Errorf("parse pr json: %w", err)
	}
	return fromGHPR(raw), nil
}

func parsePRListJSON(out string) ([]PR, error) {
	out = strings.TrimSpace(out)
	if out == "" || out == "[]" {
		return nil, nil
	}
	var raws []ghPR
	if err := json.Unmarshal([]byte(out), &raws); err != nil {
		return nil, fmt.Errorf("parse pr list json: %w", err)
	}
	out2 := make([]PR, 0, len(raws))
	for _, r := range raws {
		out2 = append(out2, fromGHPR(r))
	}
	return out2, nil
}

func fromGHPR(raw ghPR) PR {
	p := PR{
		Number: raw.Number,
		URL:    raw.URL,
		Title:  raw.Title,
		Head:   raw.HeadRef,
		Base:   raw.BaseRef,
		Draft:  raw.IsDraft,
	}
	switch strings.ToUpper(strings.TrimSpace(raw.State)) {
	case "OPEN":
		p.State = "open"
		if raw.IsDraft {
			p.State = "draft"
		}
	case "MERGED":
		p.State = "merged"
	case "CLOSED":
		p.State = "closed"
	default:
		p.State = strings.ToLower(strings.TrimSpace(raw.State))
	}
	for _, c := range raw.CheckRoll {
		link := strings.TrimSpace(c.DetailsURL)
		p.Checks = append(p.Checks, Check{
			Name:       c.Name,
			State:      normalizeCheckState(c.Status, c.Conclusion),
			Conclusion: c.Conclusion,
			URL:        link,
		})
	}
	return p
}

type ghCheck struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	Conclusion string `json:"conclusion"`
	Link       string `json:"link"`
}

func parseChecksJSON(out string) ([]Check, error) {
	out = strings.TrimSpace(out)
	if out == "" || out == "[]" {
		return nil, nil
	}
	var raws []ghCheck
	if err := json.Unmarshal([]byte(out), &raws); err != nil {
		return nil, fmt.Errorf("parse checks json: %w", err)
	}
	out2 := make([]Check, 0, len(raws))
	for _, r := range raws {
		out2 = append(out2, Check{
			Name:       r.Name,
			State:      normalizeCheckState(r.State, r.Conclusion),
			Conclusion: r.Conclusion,
			URL:        r.Link,
		})
	}
	return out2, nil
}

type ghRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

func parseRunsJSON(out string) ([]Check, error) {
	out = strings.TrimSpace(out)
	if out == "" || out == "[]" {
		return nil, nil
	}
	var raws []ghRun
	if err := json.Unmarshal([]byte(out), &raws); err != nil {
		return nil, fmt.Errorf("parse runs json: %w", err)
	}
	out2 := make([]Check, 0, len(raws))
	for _, r := range raws {
		out2 = append(out2, Check{
			Name:       r.Name,
			State:      normalizeCheckState(r.Status, r.Conclusion),
			Conclusion: r.Conclusion,
			URL:        r.URL,
		})
	}
	return out2, nil
}
