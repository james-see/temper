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

func TestEncodeClaudeProjectDir(t *testing.T) {
	got := encodeClaudeProjectDir("/Users/jc/p/temper")
	if !strings.HasPrefix(got, "Users-jc-p-temper") && got != "Users-jc-p-temper" {
		// Absolute path encoding replaces / with -
		if !strings.Contains(got, "temper") {
			t.Fatalf("%s", got)
		}
	}
	if strings.ContainsAny(got, "/\\:") {
		t.Fatalf("unexpected separators in %s", got)
	}
}

func TestParseClaudeJSONL(t *testing.T) {
	// Synthetic fixture shaped like Claude Code / Agent SDK transcripts.
	// Capture a real one later with:
	//   CLAUDE_CONFIG_DIR=/tmp/cc claude -p --output-format json "hi"
	log := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"fix auth"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"look around"},{"type":"tool_use","name":"Read","input":{"path":"a.go"}},{"type":"text","text":"checking"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"Read","content":"package a"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
		`{"type":"result","result":"all good","session_id":"abc"}`,
	}, "\n")
	ings, cur := parseClaudeJSONL(log, claudeCursor{})
	if cur.n != 5 {
		t.Fatalf("n=%d", cur.n)
	}
	var types []string
	for _, ing := range ings {
		types = append(types, ing.Type)
	}
	got := strings.Join(types, ",")
	want := "user.message,model.thinking,tool.requested,model.completed,tool.completed,model.completed,model.completed"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	more, cur2 := parseClaudeJSONL(log, cur)
	if cur2.n != 5 || len(more) != 0 {
		t.Fatalf("delta %+v %d", cur2, len(more))
	}
}

func TestParseClaudeAgentsJSON(t *testing.T) {
	out := `[{"id":"7c5dcf5d","name":"auth-refactor","cwd":"/tmp/ws","status":"running"}]`
	picks := parseClaudeAgentsJSON(out)
	if len(picks) != 1 || picks[0].SessionID != "7c5dcf5d" {
		t.Fatalf("%+v", picks)
	}
	if picks[0].Agent != "claude-code" || picks[0].Workspace != "/tmp/ws" {
		t.Fatalf("%+v", picks[0])
	}
}

func TestPickClaudeUnseen(t *testing.T) {
	seen := map[string]bool{"old": true}
	id, ok := pickClaudeUnseen([]string{"old", "new", "newer"}, seen)
	if !ok || id != "new" {
		t.Fatalf("%s %v", id, ok)
	}
}

func TestScanClaudeSessions(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "aaaa.jsonl")
	p2 := filepath.Join(dir, "bbbb.jsonl")
	if err := os.WriteFile(p1, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := os.WriteFile(p2, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ids, err := listClaudeSessionIDs(dir)
	if err != nil || len(ids) != 2 || ids[0] != "bbbb" {
		t.Fatalf("%v %v", ids, err)
	}
	if got := findClaudeSessionLog(dir, "aaaa"); got != p1 {
		t.Fatalf("%s", got)
	}
}

func TestClaudeCapabilities(t *testing.T) {
	c := NewClaudeCode("claude", false)
	cap, err := c.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !cap.ToolCalls || !cap.Resume || cap.ModelOverride {
		t.Fatalf("%+v", cap)
	}
}

func TestClaudeLaunchArgs(t *testing.T) {
	got := strings.Join(claudeLaunchArgs("", ""), " ")
	if got != "claude" {
		t.Fatal(got)
	}
	got = strings.Join(claudeLaunchArgs("fix tests", ""), " ")
	if got != "claude fix tests" {
		t.Fatal(got)
	}
	got = strings.Join(claudeLaunchArgs("", "ses-1"), " ")
	if got != "claude --resume ses-1" {
		t.Fatal(got)
	}
}

func TestClaudeMissingBinary(t *testing.T) {
	c := NewClaudeCode("claude", true)
	c.LookPath = func(string) (string, error) { return "", osErr("missing") }
	_, err := c.Start(context.Background(), TaskRequest{Workspace: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("%v", err)
	}
}

func TestClaudeBindInjectPoll(t *testing.T) {
	cfg := t.TempDir()
	ws := "/tmp/ws-claude"
	proj := filepath.Join(cfg, "projects", encodeClaudeProjectDir(ws))
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	oldID := "old-session"
	if err := os.WriteFile(filepath.Join(proj, oldID+".jsonl"), []byte(`{"type":"user","message":{"content":"old"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	c := NewClaudeCode("claude", true)
	c.ConfigDir = cfg
	c.LookPath = func(string) (string, error) { return "/bin/claude", nil }
	c.OpenTerm = func(argv []string, dir string) (*term.Handle, error) {
		if argv[0] != "claude" || dir == "" {
			t.Fatalf("open %v %s", argv, dir)
		}
		return &term.Handle{}, nil
	}
	c.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if containsArg(args, "-p") && containsArg(args, "--resume") {
			return `{"result":"ok","session_id":"x"}`, nil
		}
		return "", nil
	}

	ctx := context.Background()
	if _, err := c.Start(ctx, TaskRequest{RunID: "r1", Workspace: ws, Prompt: "fix it"}); err != nil {
		t.Fatal(err)
	}
	c.waitSnapshot(ctx)

	newID := "new-session"
	time.Sleep(20 * time.Millisecond)
	newLog := strings.Join([]string{
		`{"type":"user","message":{"content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(proj, newID+".jsonl"), []byte(newLog), 0o644); err != nil {
		t.Fatal(err)
	}

	id, ok := c.Discover(ctx)
	if !ok || id != newID {
		t.Fatalf("bind %s %v seen=%v", id, ok, c.seenIDs)
	}
	if err := c.Inject(ctx, "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	var sawPrint, sawResume bool
	for _, call := range calls {
		if containsArg(call, "-p") && containsArg(call, "Reflex replan") {
			sawPrint = true
		}
		if containsArg(call, "--resume") && containsArg(call, newID) {
			sawResume = true
		}
	}
	if !sawPrint || !sawResume {
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

func TestClaudeInjectTypesIntoTUI(t *testing.T) {
	var typed []string
	c := NewClaudeCode("claude", false)
	c.LookPath = func(string) (string, error) { return "/bin/claude", nil }
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

func TestLiveClaudePicks(t *testing.T) {
	oldAgents, oldProj := claudeAgentsJSONFn, claudeProjectDirForCWD
	defer func() {
		claudeAgentsJSONFn = oldAgents
		claudeProjectDirForCWD = oldProj
	}()

	dir := t.TempDir()
	path := filepath.Join(dir, "live-uuid.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	claudeAgentsJSONFn = func(context.Context) (string, error) {
		return `[{"id":"bg-1","name":"bg","cwd":"/tmp"}]`, nil
	}
	claudeProjectDirForCWD = func(string) string { return dir }

	picks, err := liveClaudePicks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(picks) < 2 {
		t.Fatalf("%+v", picks)
	}
	ids := map[string]bool{}
	for _, p := range picks {
		ids[p.SessionID] = true
		if p.Agent != "claude-code" {
			t.Fatalf("%+v", p)
		}
	}
	if !ids["bg-1"] || !ids["live-uuid"] {
		t.Fatalf("%v", ids)
	}
}
