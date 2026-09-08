package debuglog

import "testing"

func TestRequested(t *testing.T) {
	if !Requested(true) {
		t.Fatal("flag")
	}
	t.Setenv("TEMPER_DEBUG", "1")
	if !Requested(false) {
		t.Fatal("env")
	}
	t.Setenv("TEMPER_DEBUG", "")
	if Requested(false) {
		t.Fatal("off")
	}
}
