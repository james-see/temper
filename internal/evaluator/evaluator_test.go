package evaluator

import (
	"context"
	"testing"
)

func TestRunPassFail(t *testing.T) {
	e := Engine{Workdir: t.TempDir()}
	ok := e.Run(context.Background(), "test", "true")
	if !ok.Passed {
		t.Fatal(ok.Output)
	}
	bad := e.Run(context.Background(), "test", "false")
	if bad.Passed {
		t.Fatal("expected fail")
	}
}
