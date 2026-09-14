package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/james-see/temper/internal/term"
)

func TestParseMuseJSONL(t *testing.T) {
	log := strings.Join([]string{
		`{"sequence":1,"payload_type":"user_prompt","payload":{"text":"fix auth"}}`,
		`{"sequence":2,"payload_type":"side_effect_intent","payload":{"operation":"tool:bash","command":"ls"}}`,
		`{"sequence":3,"payload_type":"tool_batch.effect.started","payload":{"tool":"bash","args":"ls"}}`,
		`{"sequence":4,"payload_type":"tool_batch.effect.terminal","payload":{"tool":"bash","output":"a.go"}}`,
		`{"sequence":5,"payload_type":"model.completed","payload":{"text":"done","reasoning":"looked around"}}`,
		`{"sequence":6,"payload_type":"approval.review","payload":{"command":"rm -rf stale"}}`,
	}, "\n")
	ings, cur := parseMuseJSONL(log, museCursor{})
	if cur.seq != 6 {
		t.Fatalf("seq=%d", cur.seq)
	}
	var types []string
	for _, ing := range ings {
		types = append(types, ing.Type)
	}
	got := strings.Join(types, ",")
	want := "user.message,tool.requested,tool.completed,model.thinking,model.completed,user.message"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	more, cur2 := parseMuseJSONL(log, cur)
	if cur2.seq != 6 || len(more) != 0 {
		t.Fatalf("delta %+v %d", cur2, len(more))
	}
}

func TestParseMuseExport(t *testing.T) {
	out := `{
		"export_schema_version": 1,
		"events": [
			{"envelope":{"sequence":10,"payload_type":"user_prompt","payload":{"text":"hi"}}},
			{"envelope":{"sequence":11,"payload_type":"assistant.message","payload":{"event":{"kind":"assistant.message","text":"hello"}}}}
		]
	}`
	ings, cur := parseMuseExport(out, museCursor{})
	if cur.seq != 11 || len(ings) != 2 {
		t.Fatalf("%+v %+v", ings, cur)
	}
	if ings[0].Type != "user.message" || ings[1].Type != "model.completed" {
		t.Fatalf("%+v", ings)
	}
}

func TestPickMuseUnseen(t *testing.T) {
	seen := map[string]bool{"old-uuid": true}
	id, ok := pickMuseUnseen([]string{"old-uuid", "new-uuid", "newer-uuid"}, seen)
	if !ok || id != "new-uuid" {
		t.Fatalf("%s %v", id, ok)
	}
}

func TestScanMuseSessions(t *testing.T) {
	root := t.TempDir()
	p1 := filepath.Join(root, "2026", "09", "10", "aaaa-1111", "session.jsonl")
	p2 := filepath.Join(root, "2026", "09", "11", "bbbb-2222", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(p1), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p1, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(p2, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ids, err := listMuseSessionIDs(root)
	if err != nil || len(ids) != 2 || ids[0] != "bbbb-2222" {
		t.Fatalf("%v %v", ids, err)
	}
	if got := findMuseSessionLog(root, "aaaa-1111"); got != p1 {
		t.Fatalf("%s", got)
	}
}

func TestMuseCapabilities(t *testing.T) {
	m := NewMuse("muse", false)
	c, err := m.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !c.ToolCalls || !c.Resume || c.ModelOverride {
		t.Fatalf("%+v", c)
	}
}

func TestMuseBindInjectPoll(t *testing.T) {
	root := t.TempDir()
	sid := "c7dfaecc-adf6-4348-87df-95576e15136e"
	logPath := filepath.Join(root, "2026", "09", "11", sid, "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	oldLog := `{"sequence":1,"payload_type":"user_prompt","payload":{"text":"old"}}` + "\n"
	if err := os.WriteFile(logPath, []byte(oldLog), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls [][]string
	m := NewMuse("muse", true)
	m.SessionsRoot = root
	m.LookPath = func(string) (string, error) { return "/bin/muse", nil }
	m.OpenTerm = func(argv []string, dir string) (*term.Handle, error) {
		if argv[0] != "muse" || dir == "" {
			t.Fatalf("open %v %s", argv, dir)
		}
		return &term.Handle{}, nil
	}
	m.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if containsArg(args, "exec") && containsArg(args, "--session-id") {
			return "ok", nil
		}
		if containsArg(args, "export") {
			return `{"events":[]}`, nil
		}
		return "", nil
	}

	ctx := context.Background()
	if _, err := m.Start(ctx, TaskRequest{RunID: "r1", Workspace: "/tmp/ws", Prompt: "fix it"}); err != nil {
		t.Fatal(err)
	}
	m.waitSnapshot(ctx)
	// Snapshot marked the seed session; add a newer one for Discover.
	newID := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	newPath := filepath.Join(root, "2026", "09", "11", newID, "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	newLog := strings.Join([]string{
		`{"sequence":1,"payload_type":"user_prompt","payload":{"text":"hi"}}`,
		`{"sequence":2,"payload_type":"assistant.message","payload":{"text":"hello"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(newPath, []byte(newLog), 0o644); err != nil {
		t.Fatal(err)
	}

	id, ok := m.Discover(ctx)
	if !ok || id != newID {
		t.Fatalf("bind %s %v seen=%v", id, ok, m.seenIDs)
	}
	if err := m.Inject(ctx, "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	var sawExec, sawSession bool
	for _, c := range calls {
		if containsArg(c, "exec") && containsArg(c, "Reflex replan") {
			sawExec = true
		}
		if containsArg(c, "--session-id") && containsArg(c, newID) {
			sawSession = true
		}
	}
	if !sawExec || !sawSession {
		t.Fatalf("inject args %v", calls)
	}
	evs, err := m.Poll(ctx)
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

func TestMuseInjectTypesIntoTUI(t *testing.T) {
	var typed []string
	m := NewMuse("muse", false)
	m.LookPath = func(string) (string, error) { return "/bin/muse", nil }
	m.TypeTerm = func(text string) error {
		typed = append(typed, text)
		return nil
	}
	m.Bind("ses_1")
	if err := m.Inject(context.Background(), "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	if len(typed) != 1 || typed[0] != "Reflex replan" {
		t.Fatalf("%v", typed)
	}
}

func TestMuseLaunchArgs(t *testing.T) {
	got := strings.Join(museLaunchArgs("", ""), " ")
	if got != "muse" {
		t.Fatal(got)
	}
	got = strings.Join(museLaunchArgs("fix tests", ""), " ")
	if got != "muse fix tests" {
		t.Fatal(got)
	}
	got = strings.Join(museLaunchArgs("", "uuid-1"), " ")
	if got != "muse resume uuid-1" {
		t.Fatal(got)
	}
}

func TestMuseMissingBinary(t *testing.T) {
	m := NewMuse("muse", true)
	m.LookPath = func(string) (string, error) { return "", osErr("missing") }
	_, err := m.Start(context.Background(), TaskRequest{Workspace: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("%v", err)
	}
}

func TestParseMuseOpenSessionLogs(t *testing.T) {
	out := "p1234\ncmuse\nn/tmp/foo.txt\nn/home/jc/.local/share/muse/sessions/2026/09/11/abcd-efgh/session.jsonl\n"
	paths := parseMuseOpenSessionLogs(out)
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "session.jsonl") {
		t.Fatalf("%v", paths)
	}
}

func TestParseMuseRealFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/muse/session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	ings, cur := parseMuseJSONL(string(raw), museCursor{})
	if cur.seq == 0 {
		t.Fatal("expected sequences")
	}
	var types []string
	hasTool, hasApproval := false, false
	for _, ing := range ings {
		types = append(types, ing.Type)
		if ing.Type == "tool.requested" {
			hasTool = true
		}
		if ing.Type == "user.message" {
			if k, _ := ing.Data["kind"].(string); k == "approval" {
				hasApproval = true
			}
		}
	}
	if !hasTool {
		t.Fatalf("expected tool.requested in %v", types)
	}
	if !hasApproval {
		t.Fatalf("expected approval in %v", types)
	}
	// side_effect_intent must not double-count with tool_batch.effect.started
	req := 0
	for _, tpe := range types {
		if tpe == "tool.requested" {
			req++
		}
	}
	if req > 6 {
		t.Fatalf("too many tool.requested (%d) — possible intent/started double count: %v", req, types)
	}

	exp, err := os.ReadFile("testdata/muse/export.json")
	if err != nil {
		t.Fatal(err)
	}
	eings, _ := parseMuseExport(string(exp), museCursor{})
	if len(eings) == 0 {
		t.Fatal("export produced no ingests")
	}
}

func TestMuseDiscoverAfterSpawn(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "2026", "09", "10", "old-session")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "session.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMuse("muse", true)
	m.SessionsRoot = root
	m.LookPath = func(string) (string, error) { return "/bin/muse", nil }
	m.OpenTerm = func([]string, string) (*term.Handle, error) { return &term.Handle{}, nil }
	started := time.Now()
	m.Now = func() time.Time { return started }
	if _, err := m.Start(context.Background(), TaskRequest{RunID: "r1", Workspace: "/tmp/ws", Prompt: "hi"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	newDir := filepath.Join(root, "2026", "09", "11", "new-session")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newDir, "session.jsonl"), []byte(`{"sequence":1,"payload_type":"user_prompt","payload":{"text":"hi"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, ok := m.Discover(context.Background())
	if !ok || id != "new-session" {
		t.Fatalf("got %s %v seen=%v", id, ok, m.seenIDs)
	}
}

func TestMuseInjectLiveOwner(t *testing.T) {
	m := NewMuse("muse", false)
	m.LookPath = func(string) (string, error) { return "/bin/muse", nil }
	m.Bind("ses-1")
	m.Exec = func(context.Context, string, []string) (string, error) {
		return "", fmt.Errorf("muse: session already in use by interactive session")
	}
	err := m.Inject(context.Background(), "replan")
	if err == nil || !IsLiveOwner(err) {
		t.Fatalf("%v", err)
	}
}
