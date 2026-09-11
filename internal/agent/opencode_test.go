package agent

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/james-see/temper/internal/term"
)

func TestParseOpenCodeSessionIDs(t *testing.T) {
	out := `[{"id":"ses_old","title":"a"},{"id":"ses_new","title":"b"}]`
	ids := parseOpenCodeSessionIDs(out)
	if len(ids) != 2 || ids[0] != "ses_old" || ids[1] != "ses_new" {
		t.Fatalf("%v", ids)
	}
	table := `Title  ID
fix   ses_abc123
older  ses_zzz999`
	ids = parseOpenCodeSessionIDs(table)
	if len(ids) != 2 || ids[0] != "ses_abc123" {
		t.Fatalf("table %v", ids)
	}
}

func TestParseOpenCodeExport(t *testing.T) {
	out := `{
		"info": {"id":"ses_1"},
		"messages": [
			{"info":{"id":"m1","role":"user"},"parts":[{"type":"text","text":"fix auth"}]},
			{"info":{"id":"m2","role":"assistant"},"parts":[
				{"type":"reasoning","text":"look around"},
				{"type":"tool","tool":"read","state":{"status":"completed","input":{"path":"a.go"},"output":"package a"}},
				{"type":"text","text":"found it"}
			]}
		]
	}`
	ings, cur := parseOpenCodeExport(out, ocCursor{})
	if cur.n != 2 {
		t.Fatalf("n=%d", cur.n)
	}
	var types []string
	for _, ing := range ings {
		types = append(types, ing.Type)
	}
	got := strings.Join(types, ",")
	want := "user.message,model.thinking,tool.requested,tool.completed,model.completed"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	more, cur2 := parseOpenCodeExport(out, cur)
	if cur2.n != 2 || len(more) != 0 {
		t.Fatalf("delta %d %d", cur2.n, len(more))
	}
}

func TestParseOpenCodeMessageListHTTP(t *testing.T) {
	out := `[
		{"info":{"id":"u1","role":"user"},"parts":[{"type":"text","text":"hi"}]},
		{"info":{"id":"a1","role":"assistant"},"parts":[{"type":"text","text":"hello"}]}
	]`
	ings, cur := parseOpenCodeExport(out, ocCursor{})
	if cur.n != 2 || len(ings) != 2 {
		t.Fatalf("%+v %+v", ings, cur)
	}
	if ings[0].Type != "user.message" || ings[1].Type != "model.completed" {
		t.Fatalf("%+v", ings)
	}
}

func TestParseOpenCodeReasoningGrowth(t *testing.T) {
	out := `{"messages":[{"info":{"id":"1","role":"assistant"},"parts":[{"type":"reasoning","text":"plan"}]}]}`
	ings, cur := parseOpenCodeExport(out, ocCursor{})
	if len(ings) != 1 || ings[0].Type != "model.thinking" {
		t.Fatalf("%+v", ings)
	}
	grown := `{"messages":[{"info":{"id":"1","role":"assistant"},"parts":[{"type":"reasoning","text":"plan then act"}]}]}`
	more, cur2 := parseOpenCodeExport(grown, cur)
	if len(more) != 1 || more[0].Type != "model.thinking" {
		t.Fatalf("growth %+v", more)
	}
	if cur2.thinkLen != len("plan then act") {
		t.Fatalf("cursor %+v", cur2)
	}
}

func TestPickOpenCodeUnseen(t *testing.T) {
	seen := map[string]bool{"ses_old": true}
	id, ok := pickOpenCodeUnseen([]string{"ses_old", "ses_new", "ses_newer"}, seen)
	if !ok || id != "ses_new" {
		t.Fatalf("%s %v", id, ok)
	}
}

func TestOpenCodeCapabilities(t *testing.T) {
	o := NewOpenCode("opencode", false)
	c, err := o.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !c.ToolCalls || !c.Resume || !c.ModelOverride {
		t.Fatalf("%+v", c)
	}
}

func TestOpenCodeBindInjectPoll(t *testing.T) {
	var calls [][]string
	o := NewOpenCode("opencode", true)
	o.LookPath = func(string) (string, error) { return "/bin/opencode", nil }
	o.FreePort = func() (int, error) { return 4099, nil }
	o.OpenTerm = func(argv []string, dir string) (*term.Handle, error) {
		if argv[0] != "opencode" || dir == "" {
			t.Fatalf("open %v %s", argv, dir)
		}
		joined := strings.Join(argv, " ")
		if !strings.Contains(joined, "--port 4099") {
			t.Fatalf("want port in argv: %v", argv)
		}
		return &term.Handle{}, nil
	}
	listN := 0
	o.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		switch {
		case containsArg(args, "session") && containsArg(args, "list"):
			listN++
			if listN == 1 {
				return `[{"id":"ses_old"}]`, nil
			}
			return `[{"id":"ses_new"},{"id":"ses_old"}]`, nil
		case containsArg(args, "export"):
			return `{"messages":[
				{"info":{"role":"user"},"parts":[{"type":"text","text":"hi"}]},
				{"info":{"role":"assistant"},"parts":[{"type":"text","text":"hello"}]}
			]}`, nil
		case containsArg(args, "run") && containsArg(args, "--session"):
			return "ok", nil
		default:
			return "", nil
		}
	}
	// No HTTP — force CLI path for list/export/inject.
	o.HTTPDo = func(*http.Request) (*http.Response, error) {
		return nil, io.EOF
	}
	ctx := context.Background()
	if _, err := o.Start(ctx, TaskRequest{RunID: "r1", Workspace: "/tmp/ws", Prompt: "fix it"}); err != nil {
		t.Fatal(err)
	}
	id, ok := o.Discover(ctx)
	if !ok || id != "ses_new" {
		t.Fatalf("bind %s %v", id, ok)
	}
	if err := o.Inject(ctx, "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	var sawRun, sawSession bool
	for _, c := range calls {
		if containsArg(c, "run") && containsArg(c, "Reflex replan") {
			sawRun = true
		}
		if containsArg(c, "--session") && containsArg(c, "ses_new") {
			sawSession = true
		}
	}
	if !sawRun || !sawSession {
		t.Fatalf("inject args %v", calls)
	}
	evs, err := o.Poll(ctx)
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

func TestOpenCodeInjectTypesIntoTUI(t *testing.T) {
	var typed []string
	o := NewOpenCode("opencode", false)
	o.LookPath = func(string) (string, error) { return "/bin/opencode", nil }
	o.TypeTerm = func(text string) error {
		typed = append(typed, text)
		return nil
	}
	o.Bind("ses_1")
	if err := o.Inject(context.Background(), "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	if len(typed) != 1 || typed[0] != "Reflex replan" {
		t.Fatalf("%v", typed)
	}
}

func TestOpenCodeInjectHTTP(t *testing.T) {
	var paths []string
	o := NewOpenCode("opencode", false)
	o.LookPath = func(string) (string, error) { return "/bin/opencode", nil }
	o.mu.Lock()
	o.baseURL = "http://127.0.0.1:4096"
	o.sessionID = "ses_1"
	o.mu.Unlock()
	o.HTTPDo = func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(bytes.NewReader([]byte(`true`))),
			Header:     make(http.Header),
		}, nil
	}
	if err := o.Inject(context.Background(), "replan now"); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != "/tui/append-prompt" || paths[1] != "/tui/submit-prompt" {
		t.Fatalf("%v", paths)
	}
}

func TestOpenCodePollHTTP(t *testing.T) {
	o := NewOpenCode("opencode", false)
	o.LookPath = func(string) (string, error) { return "/bin/opencode", nil }
	o.Bind("ses_1")
	o.mu.Lock()
	o.baseURL = "http://127.0.0.1:4096"
	o.mu.Unlock()
	o.HTTPDo = func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/session/ses_1/message" {
			t.Fatalf("path %s", req.URL.Path)
		}
		body := `[{"info":{"role":"user"},"parts":[{"type":"text","text":"hi"}]},{"info":{"role":"assistant"},"parts":[{"type":"text","text":"yo"}]}]`
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	}
	evs, err := o.Poll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Type != "user.message" || evs[1].Type != "model.completed" {
		t.Fatalf("%+v", evs)
	}
}

func TestOpenCodeLaunchArgs(t *testing.T) {
	got := strings.Join(openCodeLaunchArgs("/tmp/ws", "", 0), " ")
	if got != "opencode" {
		t.Fatal(got)
	}
	got = strings.Join(openCodeLaunchArgs("/tmp/ws", "fix tests", 4123), " ")
	if got != "opencode --port 4123 --prompt fix tests" {
		t.Fatal(got)
	}
}

func TestOpenCodeMissingBinary(t *testing.T) {
	o := NewOpenCode("opencode", true)
	o.LookPath = func(string) (string, error) { return "", osErr("missing") }
	_, err := o.Start(context.Background(), TaskRequest{Workspace: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("%v", err)
	}
}

func TestOpenCodeStartDoesNotNeedVersion(t *testing.T) {
	o := NewOpenCode("opencode", true)
	o.LookPath = func(string) (string, error) { return "/bin/opencode", nil }
	o.FreePort = func() (int, error) { return 4001, nil }
	o.OpenTerm = func([]string, string) (*term.Handle, error) { return &term.Handle{}, nil }
	o.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		if len(args) > 0 && args[0] == "--version" {
			t.Fatal("must not block launch on opencode --version")
		}
		if containsArg(args, "session") && containsArg(args, "list") {
			return `[{"id":"ses_old"}]`, nil
		}
		return "", nil
	}
	o.HTTPDo = func(*http.Request) (*http.Response, error) { return nil, io.EOF }
	if _, err := o.Start(context.Background(), TaskRequest{Workspace: "/tmp/ws", Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	line := o.LaunchLine()
	if !strings.Contains(line, "opencode") || !strings.Contains(line, "--port") {
		t.Fatal(line)
	}
}
