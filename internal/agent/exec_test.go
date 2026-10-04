package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/term"
)

func execSpec() config.Agent {
	return config.Agent{
		Type:       "exec",
		Command:    "aider",
		Args:       []string{"--no-auto-commits", "{prompt}"},
		ResumeArgs: []string{"--restore", "{session}", "{prompt}"},
		InjectArgs: []string{"--message", "{prompt}"},
		LogFile:    "",
		Cap:        config.AgentCapabilities{Tools: true, Resume: true},
	}
}

func TestRenderExecCommand(t *testing.T) {
	spec := execSpec()
	cases := []struct {
		name                   string
		prompt, session, model string
		workspace              string
		want                   string
	}{
		{
			name:   "fresh launch renders args",
			prompt: "fix auth", workspace: "/tmp/ws",
			want: "aider --no-auto-commits fix auth",
		},
		{
			name:    "resume wins when session set",
			prompt:  "keep going",
			session: "ses-1", workspace: "/tmp/ws",
			want: "aider --restore ses-1 keep going",
		},
		{
			name:   "placeholders expand",
			prompt: "p", session: "s", model: "m", workspace: "/w",
			want: "aider --restore s p",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(RenderExecCommand(spec, "aider", tc.prompt, tc.session, tc.model, tc.workspace), " ")
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	noResume := execSpec()
	noResume.ResumeArgs = nil
	got := strings.Join(RenderExecCommand(noResume, "aider", "p", "ses-9", "", "/w"), " ")
	if got != "aider --no-auto-commits p" {
		t.Fatalf("resume without resume_args falls back to args: %q", got)
	}
}

func TestExecCapabilities(t *testing.T) {
	e := NewExec("aider", execSpec(), false)
	cap, err := e.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !cap.ToolCalls || !cap.Resume || cap.ModelOverride {
		t.Fatalf("%+v", cap)
	}
	// ResumeArgs alone imply resume support.
	plain := execSpec()
	plain.Cap.Resume = false
	e2 := NewExec("aider", plain, false)
	cap2, _ := e2.Capabilities(context.Background())
	if !cap2.Resume {
		t.Fatalf("resume_args should imply resume: %+v", cap2)
	}
	if e.ID() != "aider" {
		t.Fatalf("id %q", e.ID())
	}
}

func TestExecMissingBinary(t *testing.T) {
	e := NewExec("aider", execSpec(), true)
	e.LookPath = func(string) (string, error) { return "", osErr("missing") }
	_, err := e.Start(context.Background(), TaskRequest{Workspace: "/tmp"})
	if err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("%v", err)
	}
}

func TestExecStartBindInjectPoll(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "hist.log")
	spec := execSpec()
	spec.LogFile = logPath

	var opened []string
	var calls [][]string
	e := NewExec("aider", spec, true)
	e.LookPath = func(string) (string, error) { return "/bin/aider", nil }
	e.OpenTerm = func(argv []string, dir string) (*term.Handle, error) {
		opened = append([]string(nil), argv...)
		return &term.Handle{}, nil
	}
	e.Exec = func(_ context.Context, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "ok", nil
	}

	ctx := context.Background()
	if _, err := e.Start(ctx, TaskRequest{RunID: "r1", Workspace: "/tmp/ws", Prompt: "fix it", Session: "ses-1"}); err != nil {
		t.Fatal(err)
	}
	if e.SessionID() != "ses-1" {
		t.Fatalf("session %q", e.SessionID())
	}
	got := strings.Join(opened, " ")
	if got != "aider --restore ses-1 fix it" {
		t.Fatalf("spawn argv %q", got)
	}
	if !strings.Contains(e.LaunchLine(), "aider") {
		t.Fatalf("launch %q", e.LaunchLine())
	}

	// Inject falls back to inject_args when the handle cannot type.
	if err := e.Inject(ctx, "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || strings.Join(calls[0], " ") != "--message Reflex replan" {
		t.Fatalf("inject %v", calls)
	}

	// Queued prompts plus fresh log lines surface from Poll.
	e.Queue(Ingest{Type: "user.message", Data: map[string]any{"text": "queued"}})
	if err := os.WriteFile(logPath, []byte("first line\nsecond line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evs, err := e.Poll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, ev := range evs {
		types = append(types, ev.Type)
	}
	if strings.Join(types, ",") != "user.message,model.completed,model.completed" {
		t.Fatalf("%v", types)
	}
	if evs[1].Data["content"] != "first line" {
		t.Fatalf("%+v", evs[1].Data)
	}
	again, err := e.Poll(ctx)
	if err != nil || len(again) != 0 {
		t.Fatalf("second poll must be empty: %v %v", again, err)
	}
}

func TestExecInjectTypesIntoTUI(t *testing.T) {
	var typed []string
	e := NewExec("aider", execSpec(), false)
	e.LookPath = func(string) (string, error) { return "/bin/aider", nil }
	e.TypeTerm = func(text string) error {
		typed = append(typed, text)
		return nil
	}
	e.Bind("ses_1")
	if err := e.Inject(context.Background(), "Reflex replan"); err != nil {
		t.Fatal(err)
	}
	if len(typed) != 1 || typed[0] != "Reflex replan" {
		t.Fatalf("%v", typed)
	}
	if err := e.Interrupt(context.Background(), "ses_1"); err != nil {
		t.Fatal(err)
	}
	if len(typed) != 2 || typed[1] != "\x1b" {
		t.Fatalf("interrupt %q", typed)
	}
}

func TestExecInjectWithoutPathFails(t *testing.T) {
	spec := execSpec()
	spec.InjectArgs = nil
	e := NewExec("aider", spec, false)
	e.LookPath = func(string) (string, error) { return "/bin/aider", nil }
	if _, err := e.Start(context.Background(), TaskRequest{Workspace: "/tmp"}); err != nil {
		t.Fatal(err)
	}
	if err := e.Inject(context.Background(), "hi"); err == nil || !strings.Contains(err.Error(), "inject_args") {
		t.Fatalf("%v", err)
	}
}

func TestExecPollMissingLog(t *testing.T) {
	spec := execSpec()
	spec.LogFile = filepath.Join(t.TempDir(), "nope.log")
	e := NewExec("aider", spec, false)
	evs, err := e.Poll(context.Background())
	if err != nil || len(evs) != 0 {
		t.Fatalf("%v %v", evs, err)
	}
	if id, ok := e.Discover(context.Background()); ok || id != "" {
		t.Fatalf("unbound discover %q %v", id, ok)
	}
	e.Bind("s1")
	if id, ok := e.Discover(context.Background()); !ok || id != "s1" {
		t.Fatalf("bound discover %q %v", id, ok)
	}
}
