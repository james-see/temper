package term

import (
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
