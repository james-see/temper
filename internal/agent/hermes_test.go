package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/james-see/temper/internal/term"
)

func TestParseSessionIDs(t *testing.T) {
	out := `Title                        Workspace          Last Active   ID
────────────────────────────────────────────────────────────────
fix auth                     temper             just now      20260908_153012_abc123
older                        other              2d ago        20260905_202917_b91e99
`
	ids := parseSessionIDs(out)
	if len(ids) != 2 || ids[0] != "20260908_153012_abc123" {
		t.Fatalf("%v", ids)
	}
}

func TestParseExportLine(t *testing.T) {
	ing, ok := parseExportLine(`{"role":"user","content":"try curl"}`)
	if !ok || ing.Type != "user.message" || ing.Data["text"] != "try curl" {
		t.Fatalf("%+v %v", ing, ok)
	}
	ing, ok = parseExportLine(`{"role":"assistant","content":"ok"}`)
	if !ok || ing.Type != "model.completed" {
		t.Fatalf("%+v", ing)
	}
}

func TestClassifyLogLine(t *testing.T) {
	if _, ok := classifyLogLine(`INFO httpx2: HTTP Request: POST https://x`); ok {
		t.Fatal("skip http")
	}
	if _, ok := classifyLogLine(`INFO tool call exec terminal`); ok {
		t.Fatal("do not invent tool events from log wording")
	}
	ing, ok := classifyLogLine(`ERROR tool run failed: boom`)
	if !ok || ing.Type != "tool.completed" {
		t.Fatalf("%+v", ing)
	}
}

func TestParseExportSession(t *testing.T) {
	out := `{"id":"sess","messages":[
		{"role":"system","content":"hidden"},
		{"role":"user","content":"Why is login broken?"},
		{"role":"assistant","content":"inspecting","tool_calls":[{"function":{"name":"read_file","arguments":"{\"path\":\"auth.py\"}"}}]},
		{"role":"tool","tool_name":"read_file","content":"def login(): pass"},
		{"role":"user","content":[{"type":"text","text":"only this"}]}
	]}`
	ings, n := parseExportOutput(out, 0)
	if n != 5 {
		t.Fatalf("n=%d", n)
	}
	var types []string
	for _, ing := range ings {
		types = append(types, ing.Type)
	}
	got := strings.Join(types, ",")
	if got != "user.message,tool.requested,model.completed,tool.completed,user.message" {
		t.Fatal(got)
	}
	more, n2 := parseExportOutput(out, n)
	if n2 != 5 || len(more) != 0 {
		t.Fatalf("delta %d %d", n2, len(more))
	}
}

func TestPickUnseen(t *testing.T) {
	seen := map[string]bool{"20260901_000000_aaaaaa": true}
	id, ok := pickUnseen([]string{"20260901_000000_aaaaaa", "20260908_110000_bbbbbb", "20260908_120000_cccccc"}, seen)
	if !ok || id != "20260908_120000_cccccc" {
		t.Fatalf("%s %v", id, ok)
	}
}

func TestHermesCapabilitiesAreHonest(t *testing.T) {
	h := NewHermes("hermes", false)
	c, err := h.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.ACP || c.Resume || c.Subagents || c.ModelOverride {
		t.Fatalf("%+v", c)
	}
	if !c.ToolCalls {
		t.Fatal("observe tool calls from export")
	}
}

func TestHermesBindInjectPoll(t *testing.T) {
	var calls [][]string
	h := NewHermes("hermes", true)
	h.LookPath = func(string) (string, error) { return "/bin/hermes", nil }
	h.OpenTerm = func(argv []string, dir string) (*term.Handle, error) {
		if argv[0] != "hermes" || dir == "" {
			t.Fatalf("open %v %s", argv, dir)
		}
		return &term.Handle{}, nil
	}
	listN := 0
	h.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		switch {
		case len(args) > 0 && args[0] == "--version":
			return "hermes 0.1", nil
		case containsArg(args, "sessions") && containsArg(args, "list"):
			listN++
			if listN == 1 {
				return "old  20260901_000000_aaaaaa\n", nil
			}
			return "old  20260901_000000_aaaaaa\nnew  20260908_120000_bbbbbb\n", nil
		case containsArg(args, "export"):
			return `{"role":"user","content":"hi"}` + "\n" + `{"role":"assistant","content":"hello"}` + "\n", nil
		case containsArg(args, "logs"):
			return "ERROR tool run failed: nope\n", nil
		case containsArg(args, "chat") && containsArg(args, "--resume"):
			return "ok", nil
		default:
			return "", nil
		}
	}
	ctx := context.Background()
	if _, err := h.Start(ctx, TaskRequest{RunID: "r1", Workspace: "/tmp/ws", Prompt: ""}); err != nil {
		t.Fatal(err)
	}
	id, ok := h.Discover(ctx)
	if !ok || id != "20260908_120000_bbbbbb" {
		t.Fatalf("bind %s %v", id, ok)
	}
	var listedWorkspace bool
	for _, c := range calls {
		if containsArg(c, "--workspace") && containsArg(c, "/tmp/ws") {
			listedWorkspace = true
		}
	}
	if !listedWorkspace {
		t.Fatalf("expected full workspace path in list: %v", calls)
	}
	if err := h.Inject(ctx, "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	var sawResume, sawQ bool
	for _, c := range calls {
		if containsArg(c, "--resume") && containsArg(c, "20260908_120000_bbbbbb") {
			sawResume = true
		}
		if containsArg(c, "-q") && containsArg(c, "Reflex replan") {
			sawQ = true
		}
	}
	if !sawResume || !sawQ {
		t.Fatalf("inject args %v", calls)
	}
	evs, err := h.Poll(ctx)
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

func TestHermesStartDoesNotNeedVersion(t *testing.T) {
	h := NewHermes("hermes", true)
	h.LookPath = func(string) (string, error) { return "/bin/hermes", nil }
	h.OpenTerm = func([]string, string) (*term.Handle, error) { return &term.Handle{}, nil }
	h.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		if len(args) > 0 && args[0] == "--version" {
			t.Fatal("must not block launch on hermes --version")
		}
		if containsArg(args, "sessions") && containsArg(args, "list") {
			return "old  20260901_000000_aaaaaa\n", nil
		}
		return "", nil
	}
	if _, err := h.Start(context.Background(), TaskRequest{Workspace: "/tmp/ws"}); err != nil {
		t.Fatal(err)
	}
	line := h.LaunchLine()
	if !strings.Contains(line, "hermes --tui") || strings.Contains(line, "--source") {
		t.Fatal(line)
	}
}

func TestHermesLaunchArgs(t *testing.T) {
	got := strings.Join(hermesLaunchArgs("/tmp/ws", ""), " ")
	if got != "hermes --tui --in /tmp/ws" {
		t.Fatal(got)
	}
	got = strings.Join(hermesLaunchArgs("/tmp/ws", "fix tests"), " ")
	if got != "hermes chat --tui --in /tmp/ws -q fix tests" {
		t.Fatal(got)
	}
}

func TestHermesInjectTypesIntoTUI(t *testing.T) {
	var typed []string
	h := NewHermes("hermes", false)
	h.LookPath = func(string) (string, error) { return "/bin/hermes", nil }
	h.TypeTerm = func(text string) error {
		typed = append(typed, text)
		return nil
	}
	h.Bind("20260908_120000_bbbbbb")
	if err := h.Inject(context.Background(), "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	if len(typed) != 1 || typed[0] != "Reflex replan" {
		t.Fatalf("%v", typed)
	}
}

func TestIsLiveOwner(t *testing.T) {
	if !IsLiveOwner(fmt.Errorf("hermes: exit status 1: Session x already has a live owner (tui, pid 1)")) {
		t.Fatal("want live owner")
	}
	if IsLiveOwner(fmt.Errorf("not found")) {
		t.Fatal("other")
	}
}

func TestHermesMissingBinary(t *testing.T) {
	h := NewHermes("hermes", true)
	h.LookPath = func(string) (string, error) { return "", osErr("missing") }
	_, err := h.Start(context.Background(), TaskRequest{Workspace: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("%v", err)
	}
}

type osErr string

func (e osErr) Error() string { return string(e) }

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
