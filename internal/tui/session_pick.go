package tui

import (
	"fmt"

	"github.com/james-see/temper/internal/agent"
)

const (
	attachAllID  = "*"
	nativeRunID  = "__native__"
	spawnNewID   = "__spawn__"
	agentAllPref = "all:"
)

const (
	kindAttachAll = "all"
	kindAgentAll  = "agent-all"
	kindSession   = "session"
	kindSpawn     = "spawn"
	kindNative    = "native"
	kindHeader    = "header"
)

// AttachSpec describes how the TUI starts a run after session picking.
type AttachSpec struct {
	Agent   string
	Targets []agent.SessionPick
	Spawn   bool // spawn new sidecar for Agent
	Native  bool // continue into native provider/model/goal flow
}

func sessionPickerItems(picks []agent.SessionPick, filterAgent string, unified bool) []pickItem {
	var items []pickItem
	if len(picks) == 0 {
		return items
	}
	items = append(items, pickItem{
		ID: attachAllID, Title: "attach all", Kind: kindAttachAll, Usable: true,
		Detail: fmt.Sprintf("%d sessions", len(picks)),
	})
	order := []string{"cursor", "hermes", "opencode"}
	byAgent := map[string][]agent.SessionPick{}
	for _, p := range picks {
		a := agent.TypeOf(p.Agent)
		if a == "" {
			a = agent.Normalize(p.Agent)
		}
		byAgent[a] = append(byAgent[a], p)
	}
	for _, a := range order {
		group := byAgent[a]
		if len(group) == 0 {
			continue
		}
		if filterAgent == "" || len(byAgent) > 1 {
			items = append(items, pickItem{
				ID: "hdr:" + a, Title: "── " + a + " ──", Kind: kindHeader, Header: true, Usable: false,
			})
		}
		items = append(items, pickItem{
			ID: agentAllPref + a, Title: "all " + a, Kind: kindAgentAll, Agent: a, Usable: true,
			Detail: fmt.Sprintf("%d sessions", len(group)),
		})
		for _, p := range group {
			label := nz(p.Label, short(p.SessionID, 12))
			detail := ageSince(p.ModTime) + "  ·  " + nz(p.Preview, short(p.SessionID, 12))
			if p.Workspace != "" {
				detail = agent.WorkspaceLabel(p.Workspace) + "  ·  " + detail
			}
			items = append(items, pickItem{
				ID: p.SessionID, Title: label, Detail: detail, Meta: p.Workspace,
				Agent: a, Kind: kindSession, Usable: true,
			})
		}
	}
	if unified {
		items = append(items, pickItem{
			ID: nativeRunID, Title: "new native run", Kind: kindNative, Usable: true,
			Detail: "provider → model → goal",
		})
	} else if filterAgent != "" {
		items = append(items, pickItem{
			ID: spawnNewID, Title: "spawn new " + filterAgent, Kind: kindSpawn, Agent: filterAgent, Usable: true,
			Detail: "open a fresh session",
		})
	}
	return items
}

func expandSelectedItems(items []pickItem, picks []agent.SessionPick, selected []int) []agent.SessionPick {
	seen := map[string]bool{}
	var out []agent.SessionPick
	add := func(p agent.SessionPick) {
		key := p.Agent + "|" + p.SessionID
		if p.SessionID == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, p)
	}
	byAgent := map[string][]agent.SessionPick{}
	for _, p := range picks {
		a := agent.TypeOf(p.Agent)
		if a == "" {
			a = agent.Normalize(p.Agent)
		}
		pp := p
		pp.Agent = a
		byAgent[a] = append(byAgent[a], pp)
	}
	for _, idx := range selected {
		if idx < 0 || idx >= len(items) {
			continue
		}
		it := items[idx]
		switch it.Kind {
		case kindAttachAll:
			for _, p := range picks {
				add(normalizedPick(p))
			}
		case kindAgentAll:
			for _, p := range byAgent[it.Agent] {
				add(p)
			}
		case kindSession:
			add(agent.SessionPick{
				Agent: it.Agent, SessionID: it.ID, Workspace: it.Meta, Label: it.Title,
			})
		}
	}
	return out
}

func normalizedPick(p agent.SessionPick) agent.SessionPick {
	a := agent.TypeOf(p.Agent)
	if a == "" {
		a = agent.Normalize(p.Agent)
	}
	p.Agent = a
	return p
}

func resolveItemTargets(it pickItem, picks []agent.SessionPick) []agent.SessionPick {
	switch it.Kind {
	case kindAttachAll:
		out := make([]agent.SessionPick, 0, len(picks))
		for _, p := range picks {
			out = append(out, normalizedPick(p))
		}
		return out
	case kindAgentAll:
		var out []agent.SessionPick
		for _, p := range picks {
			if agent.TypeOf(p.Agent) == it.Agent || agent.Normalize(p.Agent) == it.Agent {
				out = append(out, normalizedPick(p))
			}
		}
		return out
	case kindSession:
		for _, p := range picks {
			if p.SessionID == it.ID {
				return []agent.SessionPick{normalizedPick(p)}
			}
		}
		return []agent.SessionPick{{
			Agent: it.Agent, SessionID: it.ID, Workspace: it.Meta, Label: it.Title,
		}}
	default:
		return nil
	}
}

func countSelected(items []pickItem) int {
	n := 0
	for _, it := range items {
		if it.Selected {
			n++
		}
	}
	return n
}

func selectedIndexes(items []pickItem) []int {
	var out []int
	for i, it := range items {
		if it.Selected {
			out = append(out, i)
		}
	}
	return out
}

func markToggle(items []pickItem, idx int) []pickItem {
	if idx < 0 || idx >= len(items) {
		return items
	}
	it := items[idx]
	if it.Header || !it.Usable {
		return items
	}
	if it.Kind == kindSpawn || it.Kind == kindNative {
		return items
	}
	items[idx].Selected = !items[idx].Selected
	return items
}

func pickerCheckMark(selected bool) string {
	if selected {
		return "[x] "
	}
	return "[ ] "
}
