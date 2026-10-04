package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCheckpointRollback(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "t@t")
	run("git", "config", "user.name", "t")
	run("git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "a.txt")
	run("git", "commit", "-m", "init")

	m, err := New(dir, filepath.Join(dir, ".temper"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Prepare("run1"); err != nil {
		t.Fatal(err)
	}
	hash, err := m.Checkpoint("c1")
	if err != nil || hash == "" {
		t.Fatalf("%s %v", hash, err)
	}
	if err := os.WriteFile(filepath.Join(m.Root(), "a.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Checkpoint("c2"); err != nil {
		t.Fatal(err)
	}
	if err := m.Rollback(hash); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(m.Root(), "a.txt"))
	if err != nil || string(b) != "one" {
		t.Fatalf("%q %v", b, err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "git", "init")
	gitRun(t, dir, "git", "config", "user.email", "t@t")
	gitRun(t, dir, "git", "config", "user.name", "t")
	gitRun(t, dir, "git", "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "git", "add", "a.txt")
	gitRun(t, dir, "git", "commit", "-m", "init")
}

func TestPrepareBranchStrategy(t *testing.T) {
	dir := t.TempDir()
	initRepo(t, dir)
	m, err := New(dir, filepath.Join(dir, ".temper"))
	if err != nil {
		t.Fatal(err)
	}
	m.Prefix = "task/"
	if err := m.PrepareBranch("r1", "", ""); err != nil {
		t.Fatal(err)
	}
	branch, err := m.CurrentBranch()
	if err != nil || branch != "task/r1" {
		t.Fatalf("branch %q %v", branch, err)
	}
	m2, err := New(dir, filepath.Join(dir, ".temper"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m2.PrepareBranch("r2", "feat/custom", ""); err != nil {
		t.Fatal(err)
	}
	branch, err = m2.CurrentBranch()
	if err != nil || branch != "feat/custom" {
		t.Fatalf("custom branch %q %v", branch, err)
	}
	if got := TaskBranch("r9", ""); got != "temper/r9" {
		t.Fatalf("default prefix %q", got)
	}
}

func TestResolveBase(t *testing.T) {
	if got := ResolveBase(t.TempDir(), "develop"); got != "develop" {
		t.Fatalf("explicit %q", got)
	}
	plain := t.TempDir()
	initRepo(t, plain)
	if got := ResolveBase(plain, ""); got != "HEAD" {
		t.Fatalf("no remote %q", got)
	}
	// Clone a file remote so origin/HEAD resolves like a real checkout.
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, t.TempDir(), "git", "init", "--bare", remote)
	src := t.TempDir()
	initRepo(t, src)
	gitRun(t, src, "git", "branch", "-M", "main")
	gitRun(t, src, "git", "remote", "add", "origin", remote)
	gitRun(t, src, "git", "push", "-u", "origin", "main")
	clone := filepath.Join(t.TempDir(), "clone")
	gitRun(t, t.TempDir(), "git", "clone", remote, clone)
	if got := ResolveBase(clone, ""); got != "origin/main" {
		t.Fatalf("origin default %q", got)
	}
}

func TestPublishRemovePruneDiskUsage(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitRun(t, t.TempDir(), "git", "init", "--bare", remote)
	src := t.TempDir()
	initRepo(t, src)
	gitRun(t, src, "git", "branch", "-M", "main")
	gitRun(t, src, "git", "remote", "add", "origin", remote)
	gitRun(t, src, "git", "push", "-u", "origin", "main")

	m, err := New(src, filepath.Join(src, ".temper"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.PrepareBranch("r1", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.Root(), "b.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Checkpoint("work"); err != nil {
		t.Fatal(err)
	}
	if err := m.Publish(""); err != nil {
		t.Fatal(err)
	}
	gitRun(t, src, "git", "--git-dir="+remote, "rev-parse", "--verify", "refs/heads/temper/r1")

	usage, err := m.DiskUsage()
	if err != nil || usage <= 0 {
		t.Fatalf("usage %d %v", usage, err)
	}
	// Orphan dir (never registered) is reported and pruned; live worktree stays.
	orphan := filepath.Join(src, ".temper", "worktrees", "ghost")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orphan, "x.bin"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	orphans, orphanBytes, err := m.Orphans()
	if err != nil || len(orphans) != 1 || orphanBytes < 4096 {
		t.Fatalf("%+v %d %v", orphans, orphanBytes, err)
	}
	removed, freed, err := m.Prune()
	if err != nil || len(removed) != 1 || freed < 4096 {
		t.Fatalf("%v %d %v", removed, freed, err)
	}
	if _, serr := os.Stat(filepath.Join(src, ".temper", "worktrees", "r1")); serr != nil {
		t.Fatalf("live worktree must survive prune: %v", serr)
	}
	if err := m.RemoveWorktree("r1"); err != nil {
		t.Fatal(err)
	}
	if _, serr := os.Stat(filepath.Join(src, ".temper", "worktrees", "r1")); !os.IsNotExist(serr) {
		t.Fatalf("worktree dir still present: %v", serr)
	}
	if err := m.RemoveWorktree("missing"); err != nil {
		t.Fatalf("missing remove must be a no-op: %v", err)
	}
}
