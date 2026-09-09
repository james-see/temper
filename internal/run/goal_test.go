package run

import "testing"

func TestClassifyTurn(t *testing.T) {
	cases := []struct {
		prev, next, kind string
	}{
		{"search the web for james-see github", "try curl instead", turnFollowup},
		{"search the web for james-see github", "also check the README", turnFollowup},
		{"fix the auth tests", "yes", turnAck},
		{"fix the auth tests", "use the staging key", turnFollowup},
		{"fix the auth tests", "can you print the error too", turnFollowup},
		{"fix the auth tests", "scratch that, write a new billing report", turnNew},
		{"fix the auth tests", "new goal: document the public API", turnNew},
		{"search github", "now do something else", turnNew},
		{"explore this repo", "forget that and implement oauth login from scratch including tokens sessions and callbacks", turnNew},
		{"list the files here", "can we run a speedtest cli please", turnNew},
		{"explore this repo", "run a speedtest", turnNew},
		{"fix the auth tests", "what time is it", turnNew},
		{"run a speedtest", "why so slow", turnFollowup},
		{"run a speedtest", "run it again", turnFollowup},
		{"", "can we run a speedtest", turnNew},
		{"fix the auth tests", "actually use the fixture instead", turnModify},
		{"how many open linear tickets do we have assigned to me", "why cant you run that then yourself for me", turnFollowup},
		{"how many open linear tickets do we have assigned to me", "that has got to be the wrong way to call it. can you try list_issues or similar", turnFollowup},
		{"how many open linear tickets do we have assigned to me", "this is hermes chat lol", turnFollowup},
		{"how many open linear tickets do we have assigned to me", "did you check the skills as well?", turnFollowup},
	}
	for _, tc := range cases {
		got := classifyTurn(tc.prev, tc.next)
		if got != tc.kind {
			t.Fatalf("%q / %q: got %s want %s", tc.prev, tc.next, got, tc.kind)
		}
		same := continuesGoal(tc.prev, tc.next)
		if same != (tc.kind != turnNew) {
			t.Fatalf("%q / %q: same=%v kind=%s", tc.prev, tc.next, same, tc.kind)
		}
	}
}
