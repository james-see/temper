package agent

import (
	"fmt"
	"path/filepath"
	"time"

	cursorconnect "github.com/james-see/cursor-connect"
)

// CursorPick is one attachable IDE composer in an open Cursor window.
type CursorPick struct {
	SessionID string
	Workspace string
	Label     string
	Preview   string
	ModTime   time.Time
}

// LiveCursorPicks lists composer sessions in currently open Cursor windows.
func LiveCursorPicks() ([]CursorPick, error) {
	c, err := cursorconnect.Open()
	if err != nil {
		return nil, err
	}
	return liveCursorPicks(c)
}

func liveCursorPicks(c *cursorconnect.Client) ([]CursorPick, error) {
	if !c.Running() {
		return nil, cursorconnect.ErrNotRunning
	}
	ss, err := c.LiveSessions()
	if err != nil {
		return nil, err
	}
	out := make([]CursorPick, 0, len(ss))
	for _, s := range ss {
		ws := s.Workspace.Path
		label := filepath.Base(ws)
		if label == "" || label == "." {
			label = s.Workspace.ID
		}
		out = append(out, CursorPick{
			SessionID: s.ID,
			Workspace: ws,
			Label:     label,
			Preview:   s.Preview,
			ModTime:   s.ModTime,
		})
	}
	return out, nil
}

// ResolveCursorPick finds a live session by id, or the sole live session.
func ResolveCursorPick(sessionID string) (CursorPick, error) {
	picks, err := LiveCursorPicks()
	if err != nil {
		return CursorPick{}, err
	}
	if sessionID != "" {
		for _, p := range picks {
			if p.SessionID == sessionID {
				return p, nil
			}
		}
		c, err := cursorconnect.Open()
		if err != nil {
			return CursorPick{}, err
		}
		s, err := c.FindSession(sessionID)
		if err != nil {
			return CursorPick{}, fmt.Errorf("cursor session %s is not in an open window", sessionID)
		}
		ws := s.Workspace.Path
		return CursorPick{SessionID: s.ID, Workspace: ws, Label: filepath.Base(ws), Preview: s.Preview, ModTime: s.ModTime}, nil
	}
	if len(picks) == 1 {
		return picks[0], nil
	}
	if len(picks) == 0 {
		return CursorPick{}, fmt.Errorf("no Cursor composer sessions in open windows")
	}
	return CursorPick{}, fmt.Errorf("cursor: %d open sessions; pick one", len(picks))
}
