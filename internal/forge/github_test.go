package forge

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func fakeGitHub(t *testing.T, out string, err error) (*GitHub, *[][]string) {
	t.Helper()
	var calls [][]string
	g := &GitHub{
		LookPath: func(string) (string, error) { return "/bin/gh", nil },
		Exec: func(_ context.Context, _ string, _ string, args []string) (string, error) {
			calls = append(calls, append([]string(nil), args...))
			return out, err
		},
	}
	return g, &calls
}

func joinArgs(calls [][]string, i int) string {
	return strings.Join(calls[i], " ")
}

func TestGitHubCreate(t *testing.T) {
	g, calls := fakeGitHub(t, `{"number":12,"url":"https://x/pr/12","title":"T","state":"OPEN","headRefName":"temper/r1","baseRefName":"main","isDraft":true}`, nil)
	pr, err := g.Create(context.Background(), "/repo", CreateOptions{Title: "T", Body: "b", Base: "main", Head: "temper/r1", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 12 || pr.State != "draft" || pr.Head != "temper/r1" || pr.Base != "main" {
		t.Fatalf("%+v", pr)
	}
	got := joinArgs(*calls, 0)
	for _, want := range []string{"pr create", "--title T", "--body b", "--base main", "--head temper/r1", "--draft"} {
		if !strings.Contains(got, want) {
			t.Fatalf("argv %q missing %q", got, want)
		}
	}
}

func TestGitHubCreateRequiresTitle(t *testing.T) {
	g, _ := fakeGitHub(t, "", nil)
	if _, err := g.Create(context.Background(), "/repo", CreateOptions{}); err == nil {
		t.Fatal("empty title must fail")
	}
}

func TestGitHubFind(t *testing.T) {
	g, calls := fakeGitHub(t, `[{"number":7,"url":"https://x/pr/7","title":"T","state":"OPEN","headRefName":"temper/r1","baseRefName":"main","isDraft":false}]`, nil)
	pr, found, err := g.Find(context.Background(), "/repo", "temper/r1")
	if err != nil || !found || pr.Number != 7 || pr.State != "open" {
		t.Fatalf("%+v %v %v", pr, found, err)
	}
	if got := joinArgs(*calls, 0); !strings.Contains(got, "pr list --head temper/r1") {
		t.Fatalf("argv %q", got)
	}
	empty, _ := fakeGitHub(t, `[]`, nil)
	if _, found, err := empty.Find(context.Background(), "/repo", "nope"); err != nil || found {
		t.Fatalf("empty list must report not-found: %v %v", found, err)
	}
}

func TestGitHubSyncIngestsChecks(t *testing.T) {
	out := `{"number":12,"url":"https://x/pr/12","title":"T","state":"OPEN","headRefName":"h","baseRefName":"main","isDraft":false,` +
		`"statusCheckRollup":[` +
		`{"name":"tests","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"https://x/1"},` +
		`{"name":"lint","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://x/2"},` +
		`{"name":"build","status":"IN_PROGRESS","conclusion":"","detailsUrl":""}` +
		`]}`
	g, _ := fakeGitHub(t, out, nil)
	pr, err := g.Sync(context.Background(), "/repo", 12)
	if err != nil {
		t.Fatal(err)
	}
	if len(pr.Checks) != 3 {
		t.Fatalf("%+v", pr.Checks)
	}
	if pr.Checks[0].State != "passing" || pr.Checks[1].State != "failing" || pr.Checks[2].State != "pending" {
		t.Fatalf("%+v", pr.Checks)
	}
	if pr.Verdict() != "failing" {
		t.Fatalf("verdict %q", pr.Verdict())
	}
	if _, err := g.Sync(context.Background(), "/repo", 0); err == nil {
		t.Fatal("zero number must fail")
	}
}

func TestGitHubChecksFallsBackToRuns(t *testing.T) {
	calls := 0
	g := &GitHub{
		LookPath: func(string) (string, error) { return "/bin/gh", nil },
		Exec: func(_ context.Context, _ string, _ string, args []string) (string, error) {
			calls++
			if args[1] == "checks" {
				return "", fmt.Errorf("no pull requests found")
			}
			return `[{"name":"CI","status":"completed","conclusion":"success","url":"https://x/r/1"}]`, nil
		},
	}
	checks, err := g.Checks(context.Background(), "/repo", "abc1234")
	if err != nil || len(checks) != 1 || checks[0].State != "passing" {
		t.Fatalf("%+v %v", checks, err)
	}
	if calls != 2 {
		t.Fatalf("expected fallback call, got %d", calls)
	}
}

func TestGitHubChecksNoFallbackForBranch(t *testing.T) {
	g, _ := fakeGitHub(t, "", fmt.Errorf("no pull requests found"))
	if _, err := g.Checks(context.Background(), "/repo", "my-branch"); err == nil {
		t.Fatal("branch miss must surface the gh error")
	}
}

func TestGitHubMissingBinary(t *testing.T) {
	g := &GitHub{LookPath: func(string) (string, error) { return "", fmt.Errorf("missing") }}
	if _, err := g.Create(context.Background(), "/repo", CreateOptions{Title: "t"}); err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("%v", err)
	}
}

func TestPRVerdict(t *testing.T) {
	if (PR{State: "open"}).Verdict() != "clean" {
		t.Fatal("open without checks is clean")
	}
	if (PR{State: "merged"}).Verdict() != "merged" {
		t.Fatal("merged passes through")
	}
	p := PR{State: "open", Checks: []Check{{State: "passing"}, {State: "pending"}}}
	if p.Verdict() != "pending" {
		t.Fatalf("verdict %q", p.Verdict())
	}
}
