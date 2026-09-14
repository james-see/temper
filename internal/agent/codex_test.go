package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/james-see/temper/internal/term"
)

func TestParseCodexJSONL(t *testing.T) {
	// Synthetic fixture shaped like Codex session rollouts + exec --json events.
	// Capture a real one later with:
	//   CODEX_HOME=/tmp/cx codex exec --json "hi"
	// then copy $CODEX_HOME/sessions/**/*.jsonl.
	log := strings.Join([]string{
		`{"type":"thread.started","thread_id":"abc"}`,
		`{"type":"user","role":"user","content":[{"type":"text","text":"fix auth"}]}`,
		`{"type":"assistant","role":"assistant","content":[{"type":"thinking","thinking":"look around"},{"type":"tool_use","name":"shell","input":{"command":"ls"}},{"type":"text","text":"checking"}]}`,
		`{"type":"item.started","item":{"type":"command_execution","command":"go test"}}`,
		`{"type":"item.completed","item":{"type":"command_execution","command":"go test","output":"ok"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`,
	}, "\n")
	ings, cur := parseCodexJSONL(log, codexCursor{})
	if cur.n != 6 {
		t.Fatalf("n=%d", cur.n)
	}
	var types []string
	for _, ing := range ings {
		types = append(types, ing.Type)
	}
	got := strings.Join(types, ",")
	want := "user.message,model.thinking,tool.requested,model.completed,tool.requested,tool.completed,model.completed"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	more, cur2 := parseCodexJSONL(log, cur)
	if cur2.n != 6 || len(more) != 0 {
		t.Fatalf("delta %+v %d", cur2, len(more))
	}
}

func TestPickCodexUnseen(t *testing.T) {
	seen := map[string]bool{"old": true}
	id, ok := pickCodexUnseen([]string{"old", "new", "newer"}, seen)
	if !ok || id != "new" {
		t.Fatalf("%s %v", id, ok)
	}
}

func TestScanCodexSessions(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "2026", "03", "14")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	p1 := filepath.Join(day, "aaaa.jsonl")
	p2 := filepath.Join(day, "rollout-bbbb.jsonl")
	if err := os.WriteFile(p1, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := os.WriteFile(p2, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ids, err := listCodexSessionIDs(root)
	if err != nil || len(ids) != 2 || ids[0] != "bbbb" {
		t.Fatalf("%v %v", ids, err)
	}
	if got := findCodexSessionLog(root, "aaaa"); got != p1 {
		t.Fatalf("%s", got)
	}
	if got := findCodexSessionLog(root, "bbbb"); got != p2 {
		t.Fatalf("%s", got)
	}
}

func TestCodexCapabilities(t *testing.T) {
	c := NewCodex("codex", false)
	cap, err := c.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !cap.ToolCalls || !cap.Resume || !cap.ModelOverride {
		t.Fatalf("%+v", cap)
	}
}

func TestCodexLaunchArgs(t *testing.T) {
	got := strings.Join(codexLaunchArgs("", "", ""), " ")
	if got != "codex" {
		t.Fatal(got)
	}
	got = strings.Join(codexLaunchArgs("fix tests", "", ""), " ")
	if got != "codex fix tests" {
		t.Fatal(got)
	}
	got = strings.Join(codexLaunchArgs("", "ses-1", ""), " ")
	if got != "codex resume ses-1" {
		t.Fatal(got)
	}
	got = strings.Join(codexLaunchArgs("hi", "", "o3"), " ")
	if got != "codex --model o3 hi" {
		t.Fatal(got)
	}
	got = strings.Join(codexLaunchArgs("", "ses-1", "o3"), " ")
	if got != "codex resume ses-1 --model o3" {
		t.Fatal(got)
	}
}

func TestCodexMissingBinary(t *testing.T) {
	c := NewCodex("codex", true)
	c.LookPath = func(string) (string, error) { return "", osErr("missing") }
	_, err := c.Start(context.Background(), TaskRequest{Workspace: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("%v", err)
	}
}

func TestCodexBindInjectPoll(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "2026", "03", "14")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	oldID := "old-session"
	if err := os.WriteFile(filepath.Join(day, oldID+".jsonl"), []byte(`{"type":"user","content":"old"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	c := NewCodex("codex", true)
	c.SessionsRoot = root
	c.LookPath = func(string) (string, error) { return "/bin/codex", nil }
	c.OpenTerm = func(argv []string, dir string) (*term.Handle, error) {
		if argv[0] != "codex" || dir == "" {
			t.Fatalf("open %v %s", argv, dir)
		}
		return &term.Handle{}, nil
	}
	c.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if containsArg(args, "exec") && containsArg(args, "resume") {
			return `{"type":"thread.started"}`, nil
		}
		return "", nil
	}

	ctx := context.Background()
	if _, err := c.Start(ctx, TaskRequest{RunID: "r1", Workspace: "/tmp/ws", Prompt: "fix it"}); err != nil {
		t.Fatal(err)
	}
	c.waitSnapshot(ctx)

	newID := "new-session"
	time.Sleep(20 * time.Millisecond)
	newLog := strings.Join([]string{
		`{"type":"user","content":[{"type":"text","text":"hi"}]}`,
		`{"type":"assistant","content":[{"type":"text","text":"hello"}]}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(day, newID+".jsonl"), []byte(newLog), 0o644); err != nil {
		t.Fatal(err)
	}

	id, ok := c.Discover(ctx)
	if !ok || id != newID {
		t.Fatalf("bind %s %v seen=%v", id, ok, c.seenIDs)
	}
	if err := c.Inject(ctx, "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	var sawExec, sawResume bool
	for _, call := range calls {
		if containsArg(call, "exec") && containsArg(call, "Reflex replan") {
			sawExec = true
		}
		if containsArg(call, "resume") && containsArg(call, newID) {
			sawResume = true
		}
	}
	if !sawExec || !sawResume {
		t.Fatalf("inject args %v", calls)
	}
	evs, err := c.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range evs {
		types = append(types, e.Type)
	}
	joined := strings.Join(types, ",")
	if !strings.Contains(joined, "user.message") || !strings.Contains(joined, "model.completed") {
		t.Fatal(joined)
	}
}

func TestCodexInjectTypesIntoTUI(t *testing.T) {
	var typed []string
	c := NewCodex("codex", false)
	c.LookPath = func(string) (string, error) { return "/bin/codex", nil }
	c.TypeTerm = func(text string) error {
		typed = append(typed, text)
		return nil
	}
	c.Bind("ses_1")
	if err := c.Inject(context.Background(), "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	if len(typed) != 1 || typed[0] != "Reflex replan" {
		t.Fatalf("%v", typed)
	}
}

func TestLiveCodexPicks(t *testing.T) {
	old := codexSessionsRootFn
	defer func() { codexSessionsRootFn = old }()

	root := t.TempDir()
	day := filepath.Join(root, "2026", "03", "14")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(day, "live-uuid.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	codexSessionsRootFn = func() string { return root }

	picks, err := liveCodexPicks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(picks) != 1 || picks[0].SessionID != "live-uuid" {
		t.Fatalf("%+v", picks)
	}
	if picks[0].Agent != "codex" {
		t.Fatalf("%+v", picks[0])
	}
}
