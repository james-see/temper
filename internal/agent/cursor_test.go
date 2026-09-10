package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	cursorconnect "github.com/james-see/cursor-connect"

	"github.com/james-see/temper/internal/event"
)

func TestIngestCursorMapping(t *testing.T) {
	ing, ok := ingestCursor(cursorconnect.Event{Kind: cursorconnect.KindUser, Text: "hi"})
	if !ok || ing.Type != event.UserMessage {
		t.Fatalf("%v %+v", ok, ing)
	}
	ing, ok = ingestCursor(cursorconnect.Event{Kind: cursorconnect.KindToolUse, ToolName: "Read", ToolInput: []byte(`{"path":"a"}`)})
	if !ok || ing.Type != event.ToolRequested || ing.Data["tool"] != "Read" {
		t.Fatalf("%+v", ing)
	}
	if _, ok = ingestCursor(cursorconnect.Event{Kind: cursorconnect.KindTurnEnded}); ok {
		t.Fatal("turn_ended should be skipped")
	}
}

func TestCursorAttachPoll(t *testing.T) {
	home := t.TempDir()
	repo := t.TempDir()
	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	wsID := cursorconnect.SanitizePath(repo)
	dir := filepath.Join(home, "projects", wsID, "agent-transcripts", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(`{"role":"user","message":{"content":[{"type":"text","text":"old"}]}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cur := NewCursor()
	cur.Home = home
	if _, err := cur.Start(context.Background(), TaskRequest{Workspace: repo}); err != nil {
		t.Fatal(err)
	}
	if cur.SessionID() != id {
		t.Fatalf("session %s", cur.SessionID())
	}
	ings, err := cur.Poll(context.Background())
	if err != nil || len(ings) != 0 {
		t.Fatalf("eof attach %v %+v", err, ings)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"role":"assistant","message":{"content":[{"type":"text","text":"done"},{"type":"tool_use","name":"Read","input":{"path":"x"}}]}}` + "\n")
	f.Close()
	ings, err = cur.Poll(context.Background())
	if err != nil || len(ings) != 2 {
		t.Fatalf("poll %v %+v", err, ings)
	}
	if ings[0].Type != event.ModelCompleted || ings[1].Type != event.ToolRequested {
		t.Fatalf("%+v", ings)
	}
}

func TestImplementedCursor(t *testing.T) {
	if !Implemented("cursor") || !IsExternal("cursor") {
		t.Fatal("cursor flags")
	}
}
