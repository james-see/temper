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
	// Prefix namespaces task branches (default "temper/").
	Prefix string
}

func New(repoRoot, dataDir string) (*Manager, error) {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, err
	}
	return &Manager{RepoRoot: abs, DataDir: dataDir}, nil
}

// Attach uses the repo root as the workspace (no isolated worktree).
func (m *Manager) Attach() {
	m.Worktree = m.RepoRoot
}

func (m *Manager) Prepare(runID string) error {
	return m.PrepareBranch(runID, "", "")
}

// PrepareBranch creates (or reattaches) the isolated worktree for runID.
// branch defaults to Prefix+runID; base defaults to the auto-resolved fork
// point (see ResolveBase). An existing worktree dir is reused as-is.
func (m *Manager) PrepareBranch(runID, branch, base string) error {
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
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = TaskBranch(runID, m.Prefix)
	}
	base = ResolveBase(m.RepoRoot, base)
	if err := run(m.RepoRoot, "git", "worktree", "add", "-b", branch, dest, base); err != nil {
		if err2 := run(m.RepoRoot, "git", "worktree", "add", dest, base); err2 != nil {
			return fmt.Errorf("worktree: %v / %v", err, err2)
		}
	}
	m.Worktree = dest
	return nil
}

// TaskBranch names the worktree branch for a run under prefix.
func TaskBranch(runID, prefix string) string {
	if strings.TrimSpace(prefix) == "" {
		prefix = "temper/"
	}
	return prefix + runID
}

// ResolveBase picks the fork point for a new task branch: the explicit base
// when given, else the remote default (origin/HEAD), else main/master when
// present, else HEAD. It returns "" when dir is not a git repo.
func ResolveBase(dir, base string) string {
	if strings.TrimSpace(base) != "" {
		return strings.TrimSpace(base)
	}
	if !isGit(dir) {
		return ""
	}
	if out, err := output(dir, "git", "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		if ref := strings.TrimSpace(out); ref != "" {
			return strings.TrimPrefix(ref, "refs/remotes/")
		}
	}
	for _, b := range []string{"origin/main", "origin/master"} {
		if run(dir, "git", "show-ref", "--verify", "--quiet", "refs/remotes/"+b) == nil {
			return b
		}
	}
	return "HEAD"
}

// CurrentBranch reports the checked-out branch of the workspace root.
func (m *Manager) CurrentBranch() (string, error) {
	out, err := output(m.Root(), "git", "branch", "--show-current")
	return strings.TrimSpace(out), err
}

// Publish pushes the workspace branch to remote (default "origin") and sets
// the upstream, so a forge PR can target it.
func (m *Manager) Publish(remote string) error {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		remote = "origin"
	}
	return run(m.Root(), "git", "push", "-u", remote, "HEAD")
}

// RemoveWorktree deletes the isolated worktree for runID and prunes the
// registration. It is a no-op when the worktree dir is absent.
func (m *Manager) RemoveWorktree(runID string) error {
	dest := filepath.Join(m.DataDir, "worktrees", runID)
	if _, err := os.Stat(dest); err != nil {
		return nil
	}
	if isGit(m.RepoRoot) {
		_ = run(m.RepoRoot, "git", "worktree", "remove", "--force", dest)
		_ = run(m.RepoRoot, "git", "worktree", "prune")
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	if m.Worktree == dest {
		m.Worktree = ""
	}
	return nil
}

// DiskUsage sums bytes under the isolated-worktrees dir. Missing dirs
// report zero without an error.
func (m *Manager) DiskUsage() (int64, error) {
	return dirSize(filepath.Join(m.DataDir, "worktrees"))
}

// Prune deletes worktree dirs that git no longer has registered and returns
// the removed paths with bytes freed.
func (m *Manager) Prune() (removed []string, freed int64, err error) {
	orphans, _, err := m.Orphans()
	if err != nil {
		return nil, 0, err
	}
	for _, o := range orphans {
		if rerr := os.RemoveAll(o.Path); rerr != nil {
			return removed, freed, rerr
		}
		removed = append(removed, o.Path)
		freed += o.Bytes
	}
	return removed, freed, nil
}

// Orphan is a worktree dir git no longer has registered.
type Orphan struct {
	Path  string
	Bytes int64
}

// Orphans lists worktree dirs git no longer has registered, with their sizes.
// It deletes nothing; Prune removes what Orphans reports.
func (m *Manager) Orphans() ([]Orphan, int64, error) {
	if isGit(m.RepoRoot) {
		_ = run(m.RepoRoot, "git", "worktree", "prune")
	}
	registered := map[string]bool{}
	if isGit(m.RepoRoot) {
		if out, oerr := output(m.RepoRoot, "git", "worktree", "list", "--porcelain"); oerr == nil {
			for _, line := range strings.Split(out, "\n") {
				if p := strings.TrimSpace(strings.TrimPrefix(line, "worktree ")); p != "" && p != line {
					// Git reports canonical paths (symlinks resolved, e.g.
					// /private/var on macOS); index both forms so callers
					// holding unresolved paths still match.
					p = filepath.Clean(p)
					registered[p] = true
					if rp, rerr := filepath.EvalSymlinks(p); rerr == nil {
						registered[rp] = true
					}
				}
			}
		}
	}
	base := filepath.Join(m.DataDir, "worktrees")
	entries, rerr := os.ReadDir(base)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, 0, nil
		}
		return nil, 0, rerr
	}
	var out []Orphan
	var total int64
	for _, e := range entries {
		p := filepath.Join(base, e.Name())
		if registered[filepath.Clean(p)] {
			continue
		}
		if rp, rerr := filepath.EvalSymlinks(p); rerr == nil && registered[rp] {
			continue
		}
		size, _ := dirSize(p)
		out = append(out, Orphan{Path: p, Bytes: size})
		total += size
	}
	return out, total, nil
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			if fi, ferr := d.Info(); ferr == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
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
	args := []string{"add", "-A"}
	if rel := dataDirRel(root, m.DataDir); rel != "" {
		// Never commit temper's own data dir (live sqlite store,
		// worktrees, artifacts): a later reset --hard would delete the
		// open database file from under the store.
		args = append(args, "--", ".", ":!"+rel)
	}
	_ = run(root, "git", args...)
	if err := run(root, "git", "-c", "user.email=temper@local", "-c", "user.name=temper", "commit", "--allow-empty", "-m", msg); err != nil {
		return "", err
	}
	out, err := output(root, "git", "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// dataDirRel returns DataDir slash-relative to root for use as a git
// exclusion pathspec, or "" when DataDir is unset, unresolvable, or outside
// root (in which case checkpoints need no exclusion).
func dataDirRel(root, dataDir string) string {
	if strings.TrimSpace(dataDir) == "" {
		return ""
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func (m *Manager) Rollback(hash string) error {
	if hash == "" {
		return fmt.Errorf("empty checkpoint")
	}
	return run(m.Root(), "git", "reset", "--hard", hash)
}

// ForkAttempt preserves the current workspace state on an attempt branch and
// resets to hash, the last-good checkpoint. It returns the branch name.
// Parallel execution is out of scope: the failed attempt survives on its
// branch while the run continues from the checkpoint with a new approach.
func (m *Manager) ForkAttempt(runID string, attempt int, hash string) (string, error) {
	if strings.TrimSpace(hash) == "" {
		return "", fmt.Errorf("empty checkpoint")
	}
	if !isGit(m.Root()) {
		return "", fmt.Errorf("not a git workspace")
	}
	if _, err := m.Checkpoint(fmt.Sprintf("temper %s fork attempt %d", runID, attempt)); err != nil {
		return "", err
	}
	branch := fmt.Sprintf("temper/%s/attempt-%d", runID, attempt)
	if err := run(m.Root(), "git", "branch", "-f", branch); err != nil {
		return "", err
	}
	if err := m.Rollback(hash); err != nil {
		return "", err
	}
	return branch, nil
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
