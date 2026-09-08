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
