package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPersistReflexMode(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "temper.yaml")
	if err := os.WriteFile(path, []byte("reflex:\n  detectors:\n    stagnation:\n      actions: 6\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ProjectConfigFile([]string{path})
	if got != path {
		t.Fatalf("project file %q", got)
	}
	if err := PersistReflexMode(path, "auto"); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config.Reflex.EffectiveMode() != ReflexAuto {
		t.Fatalf("mode %q", loaded.Config.Reflex.Mode)
	}
}

func TestEffectiveModeDefault(t *testing.T) {
	if (Reflex{}).EffectiveMode() != ReflexAuto {
		t.Fatal("default auto")
	}
	if (Reflex{Mode: "human"}).EffectiveMode() != ReflexHuman {
		t.Fatal("human")
	}
	if (Reflex{Mode: "AUTO"}).EffectiveMode() != ReflexAuto {
		t.Fatal("auto")
	}
}
