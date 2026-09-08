package agent

import (
	"context"
	"strings"
	"testing"
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
	ing, ok := classifyLogLine(`ERROR tool run failed: boom`)
	if !ok || ing.Type != "tool.completed" {
		t.Fatalf("%+v", ing)
	}
}

func TestHermesBindInjectPoll(t *testing.T) {
	var calls [][]string
	h := NewHermes("hermes", true)
	h.LookPath = func(string) (string, error) { return "/bin/hermes", nil }
	h.OpenTerm = func(argv []string, dir string) error {
		if argv[0] != "hermes" || dir == "" {
			t.Fatalf("open %v %s", argv, dir)
		}
		return nil
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
