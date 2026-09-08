package run

import "testing"

func TestContinuesGoal(t *testing.T) {
	cases := []struct {
		prev, next string
		same       bool
	}{
		{"search the web for james-see github", "try curl instead", true},
		{"search the web for james-see github", "also check the README", true},
		{"fix the auth tests", "yes", true},
		{"fix the auth tests", "use the staging key", true},
		{"fix the auth tests", "can you print the error too", true},
		{"fix the auth tests", "scratch that, write a new billing report", false},
		{"fix the auth tests", "new goal: document the public API", false},
		{"search github", "now do something else", false},
		{"explore this repo", "forget that and implement oauth login from scratch including tokens sessions and callbacks", false},
	}
	for _, tc := range cases {
		got := continuesGoal(tc.prev, tc.next)
		if got != tc.same {
			t.Fatalf("%q / %q: got same=%v want %v", tc.prev, tc.next, got, tc.same)
		}
	}
}
