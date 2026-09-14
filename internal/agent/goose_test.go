package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/james-see/temper/internal/term"
)

func TestGooseCapabilitiesAreHonest(t *testing.T) {
	g := NewGoose("goose", false)
	c, err := g.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.ACP || c.Subagents || c.Streaming || c.MCP || c.Worktrees || c.StructuredOutput {
		t.Fatalf("must not claim undriven capabilities: %+v", c)
	}
	if !c.ToolCalls || !c.Resume || !c.ModelOverride {
		t.Fatalf("must claim observed capabilities: %+v", c)
	}
}

func TestGooseLaunchArgs(t *testing.T) {
	got := strings.Join(gooseLaunchArgs("", ""), " ")
	if got != "goose session" {
		t.Fatal(got)
	}
	got = strings.Join(gooseLaunchArgs("fix tests", ""), " ")
	if got != "goose run --interactive --text fix tests" {
		t.Fatal(got)
	}
	got = strings.Join(gooseLaunchArgs("fix tests", "gpt-4o"), " ")
	if got != "goose run --interactive --text fix tests --model gpt-4o" {
		t.Fatal(got)
	}
}

func TestGooseStartSpawnsTUI(t *testing.T) {
	var opened [][]string
	g := NewGoose("goose", true)
	g.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g.OpenTerm = func(argv []string, dir string) (*term.Handle, error) {
		if argv[0] != "goose" || dir == "" {
			t.Fatalf("open %v %s", argv, dir)
		}
		opened = append(opened, argv)
		return &term.Handle{}, nil
	}
	if _, err := g.Start(context.Background(), TaskRequest{RunID: "r1", Workspace: "/tmp/ws", Prompt: "fix tests"}); err != nil {
		t.Fatal(err)
	}
	if len(opened) != 1 || !strings.Contains(strings.Join(opened[0], " "), "--text") {
		t.Fatalf("spawn args %v", opened)
	}
	if g.LaunchLine() == "" {
		t.Fatal("empty launch line")
	}
}

func TestGooseStartMissingBinary(t *testing.T) {
	g := NewGoose("goose", true)
	g.LookPath = func(string) (string, error) { return "", osErr("missing") }
	_, err := g.Start(context.Background(), TaskRequest{Workspace: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("%v", err)
	}
}

func TestGooseDiscoverPicksNewestUnseen(t *testing.T) {
	var listN int
	g := NewGoose("goose", false)
	g.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		if containsArg(args, "session") && containsArg(args, "list") {
			listN++
			if listN == 1 {
				// Snapshot: only old sessions exist before Start.
				return `{"totalSessions":1,"sessions":[{"id":"20260901_000000","working_dir":"/tmp/ws"}]}`, nil
			}
			return `{"totalSessions":2,"sessions":[
				{"id":"20260908_110000","working_dir":"/tmp/ws"},
				{"id":"20260908_120000","working_dir":"/tmp/ws"}]}`, nil
		}
		return "", nil
	}
	if _, err := g.Start(context.Background(), TaskRequest{RunID: "r1", Workspace: "/tmp/ws"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, ok := g.Discover(ctx)
	if !ok || id != "20260908_120000" {
		t.Fatalf("discover %s %v", id, ok)
	}
	// Snapshot-only: pre-existing sessions are never attached.
	g2 := NewGoose("goose", false)
	g2.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g2.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		if containsArg(args, "session") && containsArg(args, "list") {
			return `{"totalSessions":1,"sessions":[{"id":"20260901_000000","working_dir":"/tmp/ws"}]}`, nil
		}
		return "", nil
	}
	if _, err := g2.Start(context.Background(), TaskRequest{Workspace: "/tmp/ws"}); err != nil {
		t.Fatal(err)
	}
	if id, ok := g2.Discover(ctx); ok || id != "" {
		t.Fatalf("bound historical session %s %v", id, ok)
	}
}

func TestGooseDiscoverFiltersWorkspace(t *testing.T) {
	g := NewGoose("goose", false)
	g.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		return `{"totalSessions":1,"sessions":[{"id":"20260908_120000","working_dir":"/other/ws"}]}`, nil
	}
	if _, err := g.Start(context.Background(), TaskRequest{Workspace: "/tmp/ws"}); err != nil {
		t.Fatal(err)
	}
	if id, ok := g.Discover(context.Background()); ok || id != "" {
		t.Fatalf("bound foreign workspace session %s %v", id, ok)
	}
}

func TestGooseInjectTypesIntoTUI(t *testing.T) {
	var typed []string
	g := NewGoose("goose", false)
	g.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g.TypeTerm = func(text string) error {
		typed = append(typed, text)
		return nil
	}
	g.Bind("20260908_120000")
	if err := g.Inject(context.Background(), "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	if len(typed) != 1 || typed[0] != "Reflex replan" {
		t.Fatalf("%v", typed)
	}
}

func TestGooseInjectRunsResumeFallback(t *testing.T) {
	var calls [][]string
	g := NewGoose("goose", false)
	g.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if containsArg(args, "session") && containsArg(args, "list") {
			return `{"totalSessions":0,"sessions":[]}`, nil
		}
		return "", nil
	}
	g.Bind("20260908_120000")
	if err := g.Inject(context.Background(), "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	var sawResume, sawText, sawQuiet bool
	for _, c := range calls {
		if containsArg(c, "--resume") && containsArg(c, "20260908_120000") {
			sawResume = true
		}
		if containsArg(c, "--text") && containsArg(c, "Reflex replan") {
			sawText = true
		}
		if containsArg(c, "--quiet") {
			sawQuiet = true
		}
	}
	if !sawResume || !sawText || !sawQuiet {
		t.Fatalf("inject args %v", calls)
	}
}

func TestGooseInjectEmptyPrompt(t *testing.T) {
	g := NewGoose("goose", false)
	if err := g.Inject(context.Background(), "  "); err == nil {
		t.Fatal("expected empty prompt error")
	}
}

func TestGooseInjectUnbound(t *testing.T) {
	g := NewGoose("goose", false)
	g.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		return `{"totalSessions":0,"sessions":[]}`, nil
	}
	if err := g.Inject(context.Background(), "prompt"); err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("%v", err)
	}
}

func TestGoosePollExportDelta(t *testing.T) {
	var exportN int
	g := NewGoose("goose", false)
	g.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g.Bind("20260908_120000")
	g.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		if containsArg(args, "export") {
			exportN++
			if exportN == 1 {
				return gooseExportFixture, nil
			}
			return gooseExportFixture2, nil
		}
		return `{"totalSessions":0,"sessions":[]}`, nil
	}
	ctx := context.Background()
	evs, err := g.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, e := range evs {
		types = append(types, e.Type)
	}
	joined := strings.Join(types, ",")
	want := "user.message,model.thinking,tool.requested,tool.completed,model.completed"
	if joined != want {
		t.Fatalf("poll1 %s", joined)
	}
	// Second poll: transcript unchanged, no new events.
	more, err := g.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 0 {
		t.Fatalf("poll2 delta %+v", more)
	}
}

const gooseExportFixture = `{
  "id": "20260908_120000",
  "working_dir": "/tmp/ws",
  "name": "CLI Session",
  "message_count": 4,
  "conversation": [
    {"id": "m1", "role": "user", "created": 1, "content": [
      {"type": "text", "text": "why is login broken?"}
    ]},
    {"id": "m2", "role": "assistant", "created": 2, "content": [
      {"type": "thinking", "thinking": "checking auth"},
      {"type": "toolRequest", "id": "t1", "toolCall": {"name": "developer__read_file", "arguments": {"path": "auth.py"}}}
    ]},
    {"id": "m3", "role": "assistant", "created": 3, "content": [
      {"type": "toolResponse", "id": "t1", "toolResult": {"content": [{"type": "text", "text": "def login(): pass"}], "isError": false}}
    ]},
    {"id": "m4", "role": "assistant", "created": 4, "content": [
      {"type": "text", "text": "token expiry is the bug"}
    ]}
  ]
}`

const gooseExportFixture2 = gooseExportFixture

func TestGoosePollThinkingGrowth(t *testing.T) {
	base := `{"id":"s","conversation":[
		{"id":"m1","role":"assistant","created":1,"content":[
			{"type":"thinking","thinking":"plan"},
			{"type":"text","text":"working"}
		]}
	]}`
	grown := `{"id":"s","conversation":[
		{"id":"m1","role":"assistant","created":1,"content":[
			{"type":"thinking","thinking":"plan then verify the endpoint"},
			{"type":"text","text":"working"}
		]}
	]}`
	var n int
	g := NewGoose("goose", false)
	g.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g.Bind("s")
	g.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		n++
		if n == 1 {
			return base, nil
		}
		return grown, nil
	}
	ctx := context.Background()
	if _, err := g.Poll(ctx); err != nil {
		t.Fatal(err)
	}
	evs, err := g.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Type != "model.thinking" {
		t.Fatalf("growth %+v", evs)
	}
	if evs[0].Data["delta"] != len(" then verify the endpoint") {
		t.Fatalf("delta %+v", evs[0].Data)
	}
}

func TestGoosePollQueuedIngest(t *testing.T) {
	g := NewGoose("goose", false)
	g.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g.Bind("s")
	g.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		return `{"id":"s","conversation":[]}`, nil
	}
	g.Queue(Ingest{Type: "user.message", Data: map[string]any{"text": "queued"}})
	evs, err := g.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Data["text"] != "queued" {
		t.Fatalf("%+v", evs)
	}
}

func TestGooseResume(t *testing.T) {
	g := NewGoose("goose", false)
	// Unbound resume returns the current (empty) session, mirroring Muse.
	if s, err := g.Resume(context.Background(), ""); err != nil || s.ID != "" {
		t.Fatalf("%+v %v", s, err)
	}
	s, err := g.Resume(context.Background(), "20260908_120000")
	if err != nil || s.ID != "20260908_120000" {
		t.Fatalf("%+v %v", s, err)
	}
	if g.SessionID() != "20260908_120000" {
		t.Fatalf("resume did not bind: %s", g.SessionID())
	}
}

func TestGooseEventsPointsToPoll(t *testing.T) {
	g := NewGoose("goose", false)
	if _, err := g.Events(context.Background(), "s"); err == nil {
		t.Fatal("expected use Poll error")
	}
}

func TestGooseStartExecError(t *testing.T) {
	g := NewGoose("goose", false)
	g.LookPath = func(string) (string, error) { return "/bin/goose", nil }
	g.Exec = func(context.Context, string, []string) (string, error) { return "", errors.New("boom") }
	// Start does not exec when Spawn is false; exec failures surface via Poll.
	if _, err := g.Start(context.Background(), TaskRequest{Workspace: "/tmp"}); err != nil {
		t.Fatalf("non-spawn start should not exec: %v", err)
	}
}

func TestParseGooseSessionListTableFallback(t *testing.T) {
	out := `20260901_000000  old     /tmp/ws
20260908_120000  newest  /tmp/ws
`
	ids := parseGooseSessionList(out, "/tmp/ws")
	if len(ids) != 2 || ids[0] != "20260901_000000" {
		t.Fatalf("%v", ids)
	}
}

func TestPickGooseUnseen(t *testing.T) {
	seen := map[string]bool{"20260901_000000": true}
	id, ok := pickGooseUnseen([]string{"20260901_000000", "20260908_110000", "20260908_120000"}, seen)
	if !ok || id != "20260908_120000" {
		t.Fatalf("%s %v", id, ok)
	}
	if _, ok := pickGooseUnseen([]string{"20260901_000000"}, seen); ok {
		t.Fatal("all seen")
	}
}

func TestParseGooseSessionPicksRecentOnly(t *testing.T) {
	now := time.Now().UTC()
	out := `{"totalSessions":2,"sessions":[
		{"id":"20260908_120000","working_dir":"` + cwdForTest(t) + `","name":"fix auth","updated_at":"` + now.Format(time.RFC3339) + `"},
		{"id":"20260901_000000","working_dir":"` + cwdForTest(t) + `","name":"old","updated_at":"` + now.Add(-2*time.Hour).Format(time.RFC3339) + `"},
		{"id":"20260908_130000","working_dir":"/elsewhere","name":"other","updated_at":"` + now.Format(time.RFC3339) + `"}
	]}`
	picks := parseGooseSessionPicks(out)
	if len(picks) != 1 || picks[0].SessionID != "20260908_120000" || picks[0].Label != "fix auth" {
		t.Fatalf("%+v", picks)
	}
}

func cwdForTest(t *testing.T) string {
	t.Helper()
	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	return wd
}
