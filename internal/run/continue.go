package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/james-see/temper/internal/agent"
	"github.com/james-see/temper/internal/arbiter"
	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/store"
	"github.com/james-see/temper/internal/workspace"
)

// Continue resumes a non-terminal Temper run after process restart.
// It rebinds the last sidecar session from durable events and re-enters
// the external drive loop without creating a new run row.
func (m *Manager) Continue(ctx context.Context, runID string) (store.Run, error) {
	return m.ContinueWith(ctx, runID, Options{})
}

// ContinueWith is Continue with optional overrides (tests inject Sidecar).
func (m *Manager) ContinueWith(ctx context.Context, runID string, override Options) (store.Run, error) {
	r, evs, err := m.Load(ctx, runID)
	if err != nil {
		return store.Run{}, err
	}
	if terminal(r.State) {
		return r, fmt.Errorf("run %s is already %s", runID, r.State)
	}
	agentID, sessionID, wsPath := resumeBinding(evs, r)
	if agentID == "" {
		agentID = r.Agent
	}
	if override.Agent != "" {
		agentID = override.Agent
	}
	if wsPath == "" {
		wsPath = r.Workspace
	}
	if override.Root != "" {
		wsPath = override.Root
	}
	if wsPath == "" {
		wsPath, _ = os.Getwd()
	}
	opts := Options{
		Goal:           r.Goal,
		Root:           wsPath,
		Agent:          agentID,
		Provider:       r.Provider,
		Model:          r.Model,
		SkipSpawn:      true,
		Continuing:     true,
		LadderAttempts: ladderAttemptsFrom(evs),
		MaxSteps:       24,
		Sidecar:        override.Sidecar,
		Hermes:         override.Hermes,
	}
	if override.MaxSteps > 0 {
		opts.MaxSteps = override.MaxSteps
	}
	if sessionID != "" {
		opts.AttachTargets = []agent.SessionPick{{
			Agent: agentID, SessionID: sessionID, Workspace: wsPath,
		}}
		opts.CursorSession = sessionID
		opts.CursorWorkspace = wsPath
	}
	if override.Sidecar != nil {
		// Ensure the injected sidecar is bound to the resumed session.
		if b, ok := override.Sidecar.(interface{ Bind(string) }); ok && sessionID != "" {
			b.Bind(sessionID)
		}
	}

	ws, err := workspace.New(opts.Root, config.DataDir(opts.Root))
	if err != nil {
		return store.Run{}, err
	}
	ws.Attach()

	dec := arbiter.Select(m.Cfg, opts.Agent, opts.Provider, opts.Model)
	if len(opts.AttachTargets) > 0 && (dec.Selected.Agent == "" || dec.Selected.Agent == "native") {
		dec.Selected.Agent = opts.AttachTargets[0].Agent
	}
	if override.Sidecar != nil {
		dec.Selected.Agent = override.Sidecar.ID()
		opts.Agent = override.Sidecar.ID()
	}
	tried := map[string]bool{}
	if dec.Selected.Provider != "" {
		tried[dec.Selected.Provider] = true
	}

	m.rec = r
	m.seq = lastEventSeq(evs)
	m.Hydrate(r, evs)
	m.Hub.set(func(s *Snapshot) {
		s.Done = false
		s.AwaitReply = false
		s.ActiveGoal = r.Goal
		s.Agent = opts.Agent
		s.Workspace = ws.Root()
		s.ReflexMode = m.Cfg.Reflex.EffectiveMode()
	})
	m.emit(ctx, event.RunStarted, "runtime", map[string]any{
		"continued": true, "workspace": ws.Root(), "session": sessionID, "agent": opts.Agent,
	})
	return m.startExternal(ctx, opts, ws, dec, tried, r.ID)
}

func resumeBinding(evs []event.Event, r store.Run) (agentID, sessionID, workspace string) {
	agentID = r.Agent
	workspace = r.Workspace
	for _, ev := range evs {
		switch ev.Type {
		case event.SidecarBound, event.AgentStarted, event.AgentSwitched:
			var d map[string]any
			_ = json.Unmarshal(ev.Data, &d)
			if a := strings.TrimSpace(fmt.Sprint(d["agent"])); a != "" && a != "<nil>" {
				agentID = a
			}
			if a := strings.TrimSpace(fmt.Sprint(d["id"])); a != "" && a != "<nil>" && agentID == r.Agent {
				// AgentStarted uses "id" for sidecar id.
				if ev.Type == event.AgentStarted || ev.Type == event.SidecarBound {
					agentID = a
				}
			}
			if a := strings.TrimSpace(fmt.Sprint(d["to"])); a != "" && a != "<nil>" {
				agentID = a
			}
			if s := strings.TrimSpace(fmt.Sprint(d["session"])); s != "" && s != "<nil>" {
				sessionID = s
			}
			if s := strings.TrimSpace(fmt.Sprint(d["session_id"])); s != "" && s != "<nil>" {
				sessionID = s
			}
			if w := strings.TrimSpace(fmt.Sprint(d["workspace"])); w != "" && w != "<nil>" {
				workspace = w
			}
		}
	}
	return agentID, sessionID, workspace
}

func ladderAttemptsFrom(evs []event.Event) map[string]int {
	out := map[string]int{}
	for _, ev := range evs {
		if ev.Type != event.RecoveryStarted && ev.Type != event.RecoveryCompleted {
			continue
		}
		var d struct {
			Action string `json:"action"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		if d.Action != "" {
			out[d.Action]++
		}
	}
	return out
}

func lastEventSeq(evs []event.Event) uint64 {
	var n uint64
	for _, ev := range evs {
		if ev.Sequence > n {
			n = ev.Sequence
		}
	}
	return n
}

// EnqueuePrompt queues a user follow-up for delivery at the next turn boundary
// (external sidecars) or injects immediately into the native agent.
func (m *Manager) EnqueuePrompt(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("empty prompt")
	}
	if m.sess == nil {
		return fmt.Errorf("no active session")
	}
	if m.sess.native != nil {
		_, err := m.Followup(ctx, text)
		return err
	}
	if m.sess.sidecar == nil {
		return fmt.Errorf("no active session")
	}
	m.sess.prompts = append(m.sess.prompts, text)
	m.emit(ctx, event.PromptQueued, "user", map[string]any{"text": text, "queued": len(m.sess.prompts)})
	m.Hub.set(func(s *Snapshot) { s.AwaitReply = false })
	return nil
}

func (m *Manager) drainPromptQueue(ctx context.Context, emit func(string, string, any), ings []agent.Ingest) {
	if m.sess == nil || m.sess.sidecar == nil || len(m.sess.prompts) == 0 {
		return
	}
	for _, ing := range ings {
		switch ing.Type {
		case event.ToolRequested:
			m.sess.toolOpen++
		case event.ToolCompleted:
			if m.sess.toolOpen > 0 {
				m.sess.toolOpen--
			}
		case event.ModelCompleted:
			m.sess.toolOpen = 0
		}
	}
	if m.sess.toolOpen > 0 {
		return
	}
	if len(ings) > 0 && !turnBoundary(ings) {
		return
	}
	text := m.sess.prompts[0]
	if err := m.sess.sidecar.Inject(ctx, text); err != nil {
		if agent.IsLiveOwner(err) || agent.IsInjectUnavailable(err) {
			m.debug("prompt.queue.hold", "err", err.Error())
			return
		}
		m.debug("prompt.queue.err", "err", err.Error())
		return
	}
	m.sess.prompts = m.sess.prompts[1:]
	emit(event.PromptDelivered, m.sess.sidecar.ID(), map[string]any{"text": text, "remaining": len(m.sess.prompts)})
}

func turnBoundary(ings []agent.Ingest) bool {
	if len(ings) == 0 {
		return true
	}
	sawComplete := false
	openTool := false
	for _, ing := range ings {
		switch ing.Type {
		case event.ToolRequested:
			openTool = true
			sawComplete = false
		case event.ToolCompleted:
			openTool = false
		case event.ModelCompleted:
			sawComplete = true
			openTool = false
		}
	}
	return sawComplete || !openTool
}

func handoffAgentOrder() []string {
	return []string{"hermes", "opencode", "muse", "goose", "claude-code", "codex", "cursor"}
}

func pickHandoffAgent(cfg config.Config, current string) (string, bool) {
	current = agent.TypeOf(current)
	if current == "" {
		current = agent.Normalize(current)
	}
	for _, id := range handoffAgentOrder() {
		if id == current {
			continue
		}
		if !agent.Implemented(id) {
			continue
		}
		if cfg.Agents != nil {
			if a, ok := cfg.Agents[id]; ok && strings.EqualFold(a.Type, "disabled") {
				continue
			}
		}
		return id, true
	}
	return "", false
}

func (m *Manager) handoffDigest() string {
	snap := m.Hub.Get()
	var b strings.Builder
	fmt.Fprintf(&b, "Temper agent handoff.\nGoal: %s\n\nRecent events:\n", m.sess.opts.Goal)
	start := 0
	if len(snap.Events) > 20 {
		start = len(snap.Events) - 20
	}
	for _, ev := range snap.Events[start:] {
		fmt.Fprintf(&b, "- %s %s\n", ev.Type, clip(string(ev.Data), 100))
	}
	b.WriteString("\nContinue the goal in this agent. Prefer concrete next actions.\n")
	return b.String()
}

func (m *Manager) switchSidecar(ctx context.Context, to string, emit func(string, string, any)) error {
	if m.sess == nil {
		return fmt.Errorf("no session")
	}
	from := m.rec.Agent
	if from == "" && m.sess.sidecar != nil {
		from = m.sess.sidecar.ID()
	}
	opts := m.sess.opts
	opts.Agent = to
	opts.Sidecar = nil
	opts.Hermes = nil
	opts.AttachTargets = nil
	opts.CursorSession = ""
	opts.SkipSpawn = false
	opts.Continuing = false
	if m.handoffSidecar != nil {
		opts.Sidecar = m.handoffSidecar(to)
		opts.SkipSpawn = true
	}
	dec := arbiter.Select(m.Cfg, to, "", "")
	if dec.Selected.Agent == "" {
		dec.Selected.Agent = to
	}
	side, attach, err := m.openSidecar(opts, dec)
	if err != nil {
		return err
	}
	wsRoot := m.rec.Workspace
	if m.sess.ws != nil {
		wsRoot = m.sess.ws.Root()
	}
	if _, err := side.Start(ctx, agent.TaskRequest{
		RunID: m.rec.ID, Prompt: "", Workspace: wsRoot,
	}); err != nil && !strings.Contains(err.Error(), "PATH") {
		// Soft-start style: keep going for non-PATH spawn errors when SkipSpawn false.
		m.debug("handoff.start", "err", err.Error(), "to", to)
	}
	digest := m.handoffDigest()
	if err := side.Inject(ctx, digest); err != nil && !agent.IsInjectUnavailable(err) && !agent.IsLiveOwner(err) {
		m.debug("handoff.inject", "err", err.Error())
	}
	m.sess.sidecar = side
	m.sess.opts.Agent = to
	m.rec.Agent = to
	_ = m.Store.UpdateRun(ctx, m.rec)
	m.Hub.set(func(s *Snapshot) {
		s.Agent = to
		s.Attach = attach
	})
	emit(event.AgentSwitched, "reflex", map[string]any{
		"from": from, "to": to, "session": side.SessionID(),
	})
	emit(event.SidecarBound, to, map[string]any{
		"agent": to, "session": side.SessionID(), "workspace": wsRoot, "attach": attach,
	})
	m.publishSessions(side, attach)
	return nil
}
