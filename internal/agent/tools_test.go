package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadWriteConfined(t *testing.T) {
	root := t.TempDir()
	tools := NativeTools(root, time.Second, nil)
	w, ok := Lookup(tools, "write")
	if !ok {
		t.Fatal("write")
	}
	_, files, err := w.Run(context.Background(), json.RawMessage(`{"path":"a.txt","content":"hi"}`))
	if err != nil || len(files) != 1 {
		t.Fatalf("%v %v", files, err)
	}
	r, _ := Lookup(tools, "read")
	out, _, err := r.Run(context.Background(), json.RawMessage(`{"path":"a.txt"}`))
	if err != nil || out != "hi" {
		t.Fatalf("%q %v", out, err)
	}
	_, _, err = r.Run(context.Background(), json.RawMessage(`{"path":"../secret"}`))
	if err == nil {
		t.Fatal("expected escape deny")
	}
}

func TestShellDeny(t *testing.T) {
	root := t.TempDir()
	tools := NativeTools(root, time.Second, []string{"rm -rf"})
	sh, _ := Lookup(tools, "shell")
	_, _, err := sh.Run(context.Background(), json.RawMessage(`{"command":"rm -rf /"}`))
	if err == nil {
		t.Fatal("expected deny")
	}
	if err := os.WriteFile(filepath.Join(root, "x"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := sh.Run(context.Background(), json.RawMessage(`{"command":"echo ok"}`))
	if err != nil || out == "" {
		t.Fatalf("%q %v", out, err)
	}
}
