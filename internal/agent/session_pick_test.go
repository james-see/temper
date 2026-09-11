package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseHermesActiveSessionsAliveOnly(t *testing.T) {
	raw := []byte(`{"entries":[
		{"session_id":"20260909_101017_85671b","pid":36712,"surface":"cli","updated_at":1789146248.4,
		 "metadata":{"live_session_id":"20260909_101017_85671b"}},
		{"session_id":"20260908_120000_bbbbbb","pid":1,"surface":"tui","updated_at":1789140000}
	]}`)
	entries, err := parseHermesActiveSessions(raw)
	if err != nil || len(entries) != 2 {
		t.Fatalf("%+v %v", entries, err)
	}
	old := pidAliveFn
	pidAliveFn = func(pid int) bool { return pid == 1 }
	defer func() { pidAliveFn = old }()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "runtime", "active_sessions.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERMES_HOME", home)
	picks, err := liveHermesPicks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(picks) != 1 || picks[0].SessionID != "20260908_120000_bbbbbb" {
		t.Fatalf("want only alive pid lease, got %+v", picks)
	}
}

func TestParseOpenCodeListenAddrs(t *testing.T) {
	out := `COMMAND   PID USER   FD   TYPE DEVICE SIZE/OFF NODE NAME
opencode  47837 jc   10u  IPv4 0x1      0t0  TCP 127.0.0.1:4096 (LISTEN)
node      50751 jc   18u  IPv4 0x2      0t0  TCP 127.0.0.1:42286 (LISTEN)
opencode  99 jc   10u  IPv4 0x3      0t0  TCP *:4097 (LISTEN)
`
	addrs := parseOpenCodeListenAddrs(out)
	if len(addrs) != 2 || addrs[0] != "127.0.0.1:4096" || addrs[1] != "127.0.0.1:4097" {
		t.Fatalf("%v", addrs)
	}
}

func TestLiveOpenCodePicksRequiresLiveServer(t *testing.T) {
	oldFind, oldFetch := findOpenCodeListenAddrsFn, fetchOpenCodeSessionsFn
	defer func() {
		findOpenCodeListenAddrsFn = oldFind
		fetchOpenCodeSessionsFn = oldFetch
	}()
	findOpenCodeListenAddrsFn = func(context.Context) ([]string, error) { return nil, nil }
	picks, err := liveOpenCodePicks(context.Background())
	if err != nil || len(picks) != 0 {
		t.Fatalf("no server => empty %+v %v", picks, err)
	}
	findOpenCodeListenAddrsFn = func(context.Context) ([]string, error) {
		return []string{"127.0.0.1:4096"}, nil
	}
	fetchOpenCodeSessionsFn = func(_ context.Context, addr string) (string, error) {
		if addr != "127.0.0.1:4096" {
			t.Fatalf("addr %s", addr)
		}
		return `[{"id":"ses_live","title":"active"}]`, nil
	}
	picks, err = liveOpenCodePicks(context.Background())
	if err != nil || len(picks) != 1 || picks[0].SessionID != "ses_live" {
		t.Fatalf("%+v %v", picks, err)
	}
}

func TestParseOpenCodeSessionPicks(t *testing.T) {
	out := `[{"id":"ses_new","title":"Fix flaky test","time":{"updated":1710000000},"location":{"directory":"/tmp/ws"}}]`
	picks := parseOpenCodeSessionPicks(out)
	if len(picks) != 1 || picks[0].SessionID != "ses_new" {
		t.Fatalf("%+v", picks)
	}
	if picks[0].Label != "Fix flaky test" || picks[0].Workspace != "/tmp/ws" {
		t.Fatalf("%+v", picks[0])
	}
	if picks[0].ModTime.IsZero() {
		t.Fatal("expected modtime")
	}
}

func TestLiveSessionPicksFilterAndEmpty(t *testing.T) {
	oldC, oldH, oldO := probeCursor, probeHermes, probeOpenCode
	defer func() { probeCursor, probeHermes, probeOpenCode = oldC, oldH, oldO }()

	probeCursor = func() ([]SessionPick, error) {
		return []SessionPick{{Agent: "cursor", SessionID: "c1", Label: "temper"}}, nil
	}
	probeHermes = func(context.Context) ([]SessionPick, error) {
		return []SessionPick{{Agent: "hermes", SessionID: "h1", Label: "auth"}}, nil
	}
	probeOpenCode = func(context.Context) ([]SessionPick, error) {
		return nil, context.DeadlineExceeded
	}

	all, err := LiveSessionPicks(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("%d %+v", len(all), all)
	}
	only, err := LiveSessionPicks(context.Background(), "hermes")
	if err != nil || len(only) != 1 || only[0].SessionID != "h1" {
		t.Fatalf("%+v %v", only, err)
	}
}

func TestLiveSessionPicksCursorErrorWhenFiltered(t *testing.T) {
	oldC, oldH, oldO := probeCursor, probeHermes, probeOpenCode
	defer func() { probeCursor, probeHermes, probeOpenCode = oldC, oldH, oldO }()
	probeCursor = func() ([]SessionPick, error) { return nil, errNotRunning("cursor closed") }
	probeHermes = func(context.Context) ([]SessionPick, error) { return nil, nil }
	probeOpenCode = func(context.Context) ([]SessionPick, error) { return nil, nil }

	_, err := LiveSessionPicks(context.Background(), "cursor")
	if err == nil || !strings.Contains(err.Error(), "cursor") {
		t.Fatalf("%v", err)
	}
	picks, err := LiveSessionPicks(context.Background(), "")
	if err != nil || len(picks) != 0 {
		t.Fatalf("%+v %v", picks, err)
	}
}

type errNotRunning string

func (e errNotRunning) Error() string { return string(e) }

func TestCursorToSessionPickRoundTrip(t *testing.T) {
	p := CursorPick{SessionID: "a", Workspace: "/t", Label: "temper", Preview: "hi", ModTime: time.Now()}
	sp := CursorToSessionPick(p)
	if sp.Agent != "cursor" || sp.ToCursorPick().SessionID != "a" {
		t.Fatalf("%+v", sp)
	}
}
