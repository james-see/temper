package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Manager struct {
	RepoRoot string
	DataDir  string
	Worktree string
}

func New(repoRoot, dataDir string) (*Manager, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	return &Manager{RepoRoot: abs, DataDir: dataDir}, nil
}

func (m *Manager) Prepare(runID string) error {
	if err := os.MkdirAll(filepath.Join(m.DataDir, "worktrees"), 0o755); err != nil {
		return err
	}
	if !isGit(m.RepoRoot) {
		m.Worktree = m.RepoRoot
		return nil
	}
	dest := filepath.Join(m.DataDir, "worktrees", runID)
	if _, err := os.Stat(dest); err == nil {
		m.Worktree = dest
		return nil
	}
	branch := "temper/" + runID
	if err := run(m.RepoRoot, "git", "worktree", "add", "-b", branch, dest, "HEAD"); err != nil {
		if err2 := run(m.RepoRoot, "git", "worktree", "add", dest, "HEAD"); err2 != nil {
			return fmt.Errorf("worktree: %v / %v", err, err2)
		}
	}
	m.Worktree = dest
	return nil
}

func (m *Manager) Root() string {
	if m.Worktree != "" {
		return m.Worktree
	}
	return m.RepoRoot
}

func (m *Manager) Checkpoint(msg string) (string, error) {
	root := m.Root()
	if !isGit(root) {
		return "", nil
	}
	_ = run(root, "git", "add", "-A")
	if err := run(root, "git", "-c", "user.email=temper@local", "-c", "user.name=temper", "commit", "--allow-empty", "-m", msg); err != nil {
		return "", err
	}
	out, err := output(root, "git", "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

func (m *Manager) Rollback(hash string) error {
	if hash == "" {
		return fmt.Errorf("empty checkpoint")
	}
	return run(m.Root(), "git", "reset", "--hard", hash)
}

func (m *Manager) Diff() (string, error) {
	if !isGit(m.Root()) {
		return "", nil
	}
	return output(m.Root(), "git", "diff", "HEAD")
}

func (m *Manager) Status() (string, error) {
	if !isGit(m.Root()) {
		return "", nil
	}
	return output(m.Root(), "git", "status", "--porcelain")
}

func isGit(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	if err == nil {
		return true
	}
	err = run(dir, "git", "rev-parse", "--is-inside-work-tree")
	return err == nil
}

func run(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func output(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
