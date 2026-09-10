package run

import (
	"path/filepath"
	"strconv"

	"github.com/james-see/temper/internal/agent"
)

// BoundSession is one harness conversation Temper is supervising.
type BoundSession struct {
	Agent     string
	Session   string
	Workspace string
	Label     string
	Preview   string
	Attach    string
	Focus     bool
}

func describeSidecars(side agent.Sidecar, attach string) []BoundSession {
	if side == nil {
		return nil
	}
	if mux, ok := side.(*agent.Mux); ok {
		var out []BoundSession
		for i, it := range mux.Items() {
			out = append(out, describeOne(it, attach, i == 0))
		}
		return out
	}
	return []BoundSession{describeOne(side, attach, true)}
}

func describeOne(side agent.Sidecar, attach string, focus bool) BoundSession {
	b := BoundSession{
		Agent:   side.ID(),
		Session: side.SessionID(),
		Attach:  attach,
		Focus:   focus,
	}
	switch x := side.(type) {
	case *agent.Cursor:
		b.Workspace, b.Preview = x.Info()
	case *agent.Hermes:
		b.Workspace = x.Workspace()
	}
	if b.Workspace != "" {
		b.Label = filepath.Base(b.Workspace)
	}
	if b.Label == "" {
		b.Label = b.Agent
	}
	return b
}

func (m *Manager) publishSessions(side agent.Sidecar, attach string) {
	list := describeSidecars(side, attach)
	focus := ""
	for _, s := range list {
		if s.Focus && s.Session != "" {
			focus = s.Session
			break
		}
	}
	if focus == "" && len(list) > 0 {
		focus = list[0].Session
	}
	m.Hub.set(func(s *Snapshot) {
		s.Sessions = list
		s.FocusSession = focus
		if focus != "" {
			s.SessionID = focus
		}
		if n := len(list); n > 1 {
			s.Attach = attach + " × " + strconv.Itoa(n)
		}
	})
}
