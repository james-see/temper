package term

import (
	"os"
	"strings"
	"testing"
)

func TestQuoteAndCommandLine(t *testing.T) {
	got := CommandLine([]string{"hermes", "--tui", "--in", "/tmp/my dir"}, "/tmp/my dir")
	if !strings.Contains(got, "cd '/tmp/my dir'") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "hermes --tui --in '/tmp/my dir'") {
		t.Fatal(got)
	}
}

func TestResolveApp(t *testing.T) {
	t.Setenv("TEMPER_TERMINAL", "iTerm.app")
	if ResolveApp() != "iTerm" {
		t.Fatal(ResolveApp())
	}
	t.Setenv("TEMPER_TERMINAL", "")
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if ResolveApp() != "iTerm" {
		t.Fatal(ResolveApp())
	}
}

func TestParseTTY(t *testing.T) {
	if got := parseTTY("ok\n/dev/ttys012\n"); got != "/dev/ttys012" {
		t.Fatal(got)
	}
	if parseTTY("nothing") != "" {
		t.Fatal("empty")
	}
}

func TestAppleScriptReturnsTTY(t *testing.T) {
	s := appleScript("iTerm", "hermes --tui")
	if !strings.Contains(s, "return tty") || !strings.Contains(s, "write text") {
		t.Fatal(s)
	}
	s = appleScript("Terminal", "hermes --tui")
	if !strings.Contains(s, "return tty of newTab") {
		t.Fatal(s)
	}
}

func TestTypeITermScript(t *testing.T) {
	s := typeITermScript("/dev/ttys003", `replan "auth"`)
	if !strings.Contains(s, `/dev/ttys003`) || !strings.Contains(s, `replan \"auth\"`) {
		t.Fatal(s)
	}
	if !strings.Contains(s, "write text") {
		t.Fatal(s)
	}
}

func TestTypeTTY(t *testing.T) {
	p := t.TempDir() + "/pty"
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := typeTTY(p, "hello\n"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello\r" {
		t.Fatalf("%q", b)
	}
}

func TestHandleCanType(t *testing.T) {
	var h *Handle
	if h.CanType() {
		t.Fatal("nil")
	}
	if (&Handle{}).CanType() {
		t.Fatal("empty")
	}
	if !(&Handle{TTY: "/dev/ttys001"}).CanType() {
		t.Fatal("want can")
	}
}
