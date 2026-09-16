package run

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/james-see/temper/internal/agent"
	"github.com/james-see/temper/internal/arbiter"
	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/provider"
	"github.com/james-see/temper/internal/reflex"
	"github.com/james-see/temper/internal/store"
	"github.com/james-see/temper/internal/workspace"
)

func (m *Manager) startExternal(ctx context.Context, opts Options, ws *workspace.Manager, dec arbiter.Decision, tried map[string]bool, id string) (store.Run, error) {
	m.rec.Provider = ""
	m.rec.Model = ""
	m.rec.Workspace = ws.Root()
	_ = m.Store.UpdateRun(ctx, m.rec)
	m.Hub.set(func(s *Snapshot) {
		s.Provider = ""
		s.Model = ""
		s.Workspace = ws.Root()
		s.Attach = "session+logs"
		s.JudgeOn = m.Cfg.Reflex.Judge.EffectiveEnabled()
	})

	side, attach, err := m.openSidecar(opts, dec)
	if err != nil {
		return m.fail(ctx, err)
	}
	m.Hub.set(func(s *Snapshot) { s.Attach = attach })

	reqWS := ws.Root()
	if opts.CursorWorkspace != "" {
		reqWS = opts.CursorWorkspace
	}
	if side.SessionID() != "" {
		if !opts.Continuing {
			if p := strings.TrimSpace(opts.Goal); p != "" {
				if err := side.Inject(ctx, p); err != nil && !agent.IsInjectUnavailable(err) {
					return m.fail(ctx, err)
				}
			}
		}
	} else if _, err := side.Start(ctx, agent.TaskRequest{
		RunID: id, Prompt: opts.Goal, Workspace: reqWS, Session: opts.CursorSession,
	}); err != nil {
		soft := (side.ID() == "hermes" || side.ID() == "opencode" || side.ID() == "muse" || side.ID() == "goose" || side.ID() == "claude-code" || side.ID() == "codex") && !strings.Contains(err.Error(), "PATH")
		if soft {
			m.debug("sidecar.start", "err", err.Error(), "cmd", side.LaunchLine(), "id", side.ID())
		} else {
			return m.fail(ctx, err)
		}
	}

	if err := m.transition(ctx, StatePlanned); err != nil {
		return m.rec, err
	}
	emit := func(typ, actor string, data any) { m.emit(ctx, typ, actor, data) }
	emit(event.PlanCreated, side.ID(), map[string]any{"plan": "sidecar control plane"})
	emit(event.AgentStarted, side.ID(), map[string]any{
		"id": side.ID(), "command": side.LaunchLine(), "spawn": !opts.SkipSpawn, "attach": attach,
	})
	if sid := side.SessionID(); sid != "" {
		emit(event.SidecarBound, side.ID(), map[string]any{
			"agent": side.ID(), "session": sid, "workspace": reqWS, "attach": attach,
		})
	}
	if opts.SkipSpawn {
		m.debug("sidecar.plain", "cmd", side.LaunchLine(), "id", side.ID())
	}
	m.publishSessions(side, attach)

	eng := reflex.NewEngine(
		m.Cfg.Reflex.Detectors.ActionCycle.Repetitions,
		m.Cfg.Reflex.Detectors.RepeatedError.Threshold,
		m.Cfg.Reflex.Detectors.TokenBurn.Threshold,
		m.Cfg.Reflex.Detectors.Stagnation.Actions,
		m.Cfg.Reflex.Detectors.Regression.Enabled,
	)
	ladder := reflex.NewLadder(sidecarRecovery(m.Cfg))
	if caps, err := side.Capabilities(ctx); err == nil && caps.ModelOverride {
		ladder = reflex.NewLadder(sidecarRecoveryCaps(m.Cfg, true))
	}
	for action, n := range opts.LadderAttempts {
		if n > 0 {
			ladder.Attempts[action] = n
		}
	}
	m.sess = &session{
		sidecar: side,
		ws:      ws,
		opts:    opts,
		tried:   tried,
		eng:     eng,
		ladder:  ladder,
		approve: make(chan struct{}, 1),
		mode:    m.Cfg.Reflex.EffectiveMode(),
	}
	m.debug("routing", "agent", m.rec.Agent, "sidecar", true, "spawn", !opts.SkipSpawn, "attach", attach, "continue", opts.Continuing)
	return m.driveExternal(ctx)
}

func (m *Manager) openSidecar(opts Options, dec arbiter.Decision) (agent.Sidecar, string, error) {
	if opts.Sidecar != nil {
		return opts.Sidecar, attachMode(opts.Sidecar.ID()), nil
	}
	if opts.Hermes != nil {
		if opts.SkipSpawn {
			opts.Hermes.Spawn = false
		}
		return opts.Hermes, "session+logs", nil
	}
	targets, err := resolveAttachTargets(opts, dec.Selected.Agent)
	if err != nil {
		return nil, "", err
	}
	if len(targets) > 0 {
		return m.openAttached(opts, targets)
	}
	typ := agent.TypeOf(dec.Selected.Agent)
	switch typ {
	case "hermes":
		return agent.NewHermes(agentCommand(m.Cfg, dec.Selected.Agent, "hermes"), !opts.SkipSpawn), "session+logs", nil
	case "opencode":
		return agent.NewOpenCode(agentCommand(m.Cfg, dec.Selected.Agent, "opencode"), !opts.SkipSpawn), "session+logs", nil
	case "muse":
		return agent.NewMuse(agentCommand(m.Cfg, dec.Selected.Agent, "muse"), !opts.SkipSpawn), "session+logs", nil
	case "goose":
		return agent.NewGoose(agentCommand(m.Cfg, dec.Selected.Agent, "goose"), !opts.SkipSpawn), "session+logs", nil
	case "claude-code":
		return agent.NewClaudeCode(agentCommand(m.Cfg, dec.Selected.Agent, "claude"), !opts.SkipSpawn), "session+logs", nil
	case "codex":
		return agent.NewCodex(agentCommand(m.Cfg, dec.Selected.Agent, "codex"), !opts.SkipSpawn), "session+logs", nil
	case "cursor":
		return m.openCursor(opts)
	default:
		return nil, "", agent.UnimplementedError(dec.Selected.Agent)
	}
}

func agentCommand(cfg config.Config, selected, fallback string) string {
	cmd := fallback
	if a, ok := cfg.Agents[selected]; ok && a.Command != "" {
		return a.Command
	}
	if a, ok := cfg.Agents[fallback]; ok && a.Command != "" {
		return a.Command
	}
	return cmd
}

func (m *Manager) openAttached(opts Options, targets []agent.SessionPick) (agent.Sidecar, string, error) {
	var items []agent.Sidecar
	modes := map[string]bool{}
	for _, t := range targets {
		side, mode, err := m.startAttached(opts, t)
		if err != nil {
			return nil, "", err
		}
		items = append(items, side)
		modes[mode] = true
	}
	attach := "session+logs"
	if len(modes) == 1 && modes["ide-transcripts"] {
		attach = "ide-transcripts"
	} else if len(modes) > 1 {
		attach = "multi"
	}
	if len(items) == 1 {
		return items[0], attach, nil
	}
	name := "attach"
	if len(targets) > 0 {
		agents := map[string]bool{}
		for _, t := range targets {
			agents[agent.TypeOf(t.Agent)] = true
		}
		if len(agents) == 1 {
			for a := range agents {
				name = a
			}
		}
	}
	return agent.NewMux(name, items), attach, nil
}

func (m *Manager) startAttached(opts Options, t agent.SessionPick) (agent.Sidecar, string, error) {
	typ := agent.TypeOf(t.Agent)
	if typ == "" {
		typ = agent.Normalize(t.Agent)
	}
	ws := t.Workspace
	if ws == "" {
		ws = opts.Root
	}
	req := agent.TaskRequest{Workspace: ws, Session: t.SessionID, Prompt: opts.Goal}
	switch typ {
	case "cursor":
		c := agent.NewCursor()
		if _, err := c.Start(context.Background(), req); err != nil {
			return nil, "", err
		}
		return c, "ide-transcripts", nil
	case "hermes":
		h := agent.NewHermes(agentCommand(m.Cfg, t.Agent, "hermes"), false)
		if _, err := h.Start(context.Background(), req); err != nil {
			return nil, "", err
		}
		return h, "session+logs", nil
	case "opencode":
		o := agent.NewOpenCode(agentCommand(m.Cfg, t.Agent, "opencode"), false)
		if _, err := o.Start(context.Background(), req); err != nil {
			return nil, "", err
		}
		return o, "session+logs", nil
	case "muse":
		mu := agent.NewMuse(agentCommand(m.Cfg, t.Agent, "muse"), false)
		if _, err := mu.Start(context.Background(), req); err != nil {
			return nil, "", err
		}
		return mu, "session+logs", nil
	case "goose":
		g := agent.NewGoose(agentCommand(m.Cfg, t.Agent, "goose"), false)
		if _, err := g.Start(context.Background(), req); err != nil {
			return nil, "", err
		}
		return g, "session+logs", nil
	case "claude-code":
		cc := agent.NewClaudeCode(agentCommand(m.Cfg, t.Agent, "claude"), false)
		if _, err := cc.Start(context.Background(), req); err != nil {
			return nil, "", err
		}
		return cc, "session+logs", nil
	case "codex":
		cx := agent.NewCodex(agentCommand(m.Cfg, t.Agent, "codex"), false)
		if _, err := cx.Start(context.Background(), req); err != nil {
			return nil, "", err
		}
		return cx, "session+logs", nil
	default:
		return nil, "", agent.UnimplementedError(t.Agent)
	}
}

func (m *Manager) openCursor(opts Options) (agent.Sidecar, string, error) {
	targets, err := resolveCursorTargets(opts)
	if err != nil {
		return nil, "", err
	}
	if len(targets) == 0 {
		return agent.NewCursor(), "ide-transcripts", nil
	}
	picks := make([]agent.SessionPick, 0, len(targets))
	for _, t := range targets {
		picks = append(picks, agent.CursorToSessionPick(t))
	}
	return m.openAttached(opts, picks)
}

func resolveAttachTargets(opts Options, selectedAgent string) ([]agent.SessionPick, error) {
	if len(opts.AttachTargets) > 0 {
		return opts.AttachTargets, nil
	}
	// Legacy cursor-only fields.
	cps, err := resolveCursorTargets(opts)
	if err != nil {
		return nil, err
	}
	if len(cps) == 0 {
		// --session for hermes/opencode without AttachTargets
		sess := strings.TrimSpace(opts.CursorSession)
		if sess == "" || sess == "*" || strings.EqualFold(sess, "all") {
			return nil, nil
		}
		typ := agent.TypeOf(selectedAgent)
		if typ == "hermes" || typ == "opencode" || typ == "muse" || typ == "goose" || typ == "claude-code" || typ == "codex" {
			return []agent.SessionPick{{
				Agent: typ, SessionID: sess, Workspace: opts.CursorWorkspace,
			}}, nil
		}
		return nil, nil
	}
	out := make([]agent.SessionPick, 0, len(cps))
	for _, p := range cps {
		out = append(out, agent.CursorToSessionPick(p))
	}
	return out, nil
}

func resolveCursorTargets(opts Options) ([]agent.CursorPick, error) {
	if len(opts.CursorTargets) > 0 {
		return opts.CursorTargets, nil
	}
	sess := strings.TrimSpace(opts.CursorSession)
	all := opts.CursorAttachAll || sess == "*" || strings.EqualFold(sess, "all")
	if all {
		typ := agent.TypeOf(opts.Agent)
		if typ != "" && typ != "cursor" {
			return nil, nil
		}
		return agent.LiveCursorPicks()
	}
	if sess != "" {
		typ := agent.TypeOf(opts.Agent)
		if typ == "hermes" || typ == "opencode" || typ == "muse" || typ == "goose" || typ == "claude-code" || typ == "codex" {
			return nil, nil
		}
		if opts.CursorWorkspace != "" {
			return []agent.CursorPick{{SessionID: sess, Workspace: opts.CursorWorkspace}}, nil
		}
		p, err := agent.ResolveCursorPick(sess)
		if err != nil {
			return nil, err
		}
		return []agent.CursorPick{p}, nil
	}
	return nil, nil
}

func attachMode(id string) string {
	if id == "cursor" {
		return "ide-transcripts"
	}
	if id == "attach" || id == "mux" {
		return "multi"
	}
	return "session+logs"
}

func (m *Manager) driveExternal(ctx context.Context) (store.Run, error) {
	s := m.sess
	h := s.sidecar
	eng := s.eng
	emit := func(typ, actor string, data any) { m.emit(ctx, typ, actor, data) }
	bound := false
	var lastState reflex.ProgressState
	var lastReasons string
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		if err := ctx.Err(); err != nil {
			_ = h.Interrupt(ctx, h.SessionID())
			return m.cancel(ctx)
		}
		if s.pending == nil {
			if err := m.ensureState(ctx, StateExecuting); err != nil {
				return m.rec, err
			}
		}

		ings, pollErr := h.Poll(ctx)
		if pollErr != nil {
			m.debug("sidecar.poll", "err", pollErr.Error(), "id", h.ID())
		}
		userOnly := sidecarUserOnly(ings)
		goalShift := false
		approvalPending := false
		for i, ing := range ings {
			if ing.Type == event.UserMessage {
				text := strings.TrimSpace(fmt.Sprint(ing.Data["text"]))
				origKind, _ := ing.Data["kind"].(string)
				if systemNoise(text) {
					ing.Data["kind"] = "ack"
					ing.Data["same_goal"] = true
					ings[i] = ing
					emit(ing.Type, h.ID(), ing.Data)
					continue
				}
				if ing.Data == nil {
					ing.Data = map[string]any{}
				}
				if origKind == "approval" {
					ing.Data["kind"] = "approval"
					ing.Data["same_goal"] = true
					approvalPending = true
					ings[i] = ing
					emit(ing.Type, h.ID(), ing.Data)
					continue
				}
				kind := classifyTurn(s.opts.Goal, text)
				ing.Data["kind"] = kind
				ing.Data["same_goal"] = kind != turnNew
				ings[i] = ing
				if m.adoptUserGoal(ctx, text, kind) {
					goalShift = true
				}
				if kind == turnNew {
					eng.Reset()
					s.stagnation = 0
					s.thinkChars = 0
					s.step = 0
					s.phaseCompl = 0
					s.phasePrompt = 0
				}
			}
			emit(ing.Type, h.ID(), ing.Data)
		}
		if approvalPending {
			m.Hub.set(func(snap *Snapshot) {
				snap.PendingRecovery = "approval"
				snap.PendingReasons = []string{"approval"}
			})
			_ = m.transition(ctx, StateWaitingHuman)
			select {
			case <-ctx.Done():
				_ = h.Interrupt(ctx, h.SessionID())
				return m.cancel(ctx)
			case <-s.approve:
				if err := m.applyPending(ctx, emit); err != nil {
					return m.rec, err
				}
			case <-tick.C:
			}
			continue
		}
		if goalShift {
			lastState = ""
			lastReasons = ""
		}
		actionNorms, errFPs, meaningful, thinkDelta, _, thinkText := sidecarSignals(ings)
		s.thinkChars += thinkDelta
		thinkOnly := s.thinkChars > 0 && len(actionNorms) == 0 && !meaningful
		if id := h.SessionID(); id != "" {
			m.publishSessions(h, attachMode(h.ID()))
			if !bound {
				bound = true
				emit(event.AgentStarted, h.ID(), map[string]any{"id": h.ID(), "session": id})
				emit(event.SidecarBound, h.ID(), map[string]any{
					"agent": h.ID(), "session": id, "workspace": m.rec.Workspace, "attach": attachMode(h.ID()),
				})
			}
		}

		m.drainPromptQueue(ctx, emit, ings)

		// Wait for Hermes to come up — do not assess or spam starting events.
		if !bound && len(ings) == 0 {
			select {
			case <-ctx.Done():
				_ = h.Interrupt(ctx, h.SessionID())
				return m.cancel(ctx)
			case <-s.approve:
				if err := m.applyPending(ctx, emit); err != nil {
					return m.rec, err
				}
			case <-tick.C:
			}
			continue
		}

		// Empty 1s polls are not a turn. Thinking growth still arrives as ingest
		// when Hermes flushes or updates the last assistant row.
		if len(ings) == 0 {
			m.drainPromptQueue(ctx, emit, ings)
			if m.shouldApplyPending() {
				if err := m.applyPending(ctx, emit); err != nil {
					return m.rec, err
				}
			}
			select {
			case <-ctx.Done():
				_ = h.Interrupt(ctx, h.SessionID())
				return m.cancel(ctx)
			case <-s.approve:
				if err := m.applyPending(ctx, emit); err != nil {
					return m.rec, err
				}
			case <-tick.C:
			}
			continue
		}

		if userOnly {
			if m.shouldApplyPending() {
				if err := m.applyPending(ctx, emit); err != nil {
					return m.rec, err
				}
			}
			select {
			case <-ctx.Done():
				_ = h.Interrupt(ctx, h.SessionID())
				return m.cancel(ctx)
			case <-s.approve:
				if err := m.applyPending(ctx, emit); err != nil {
					return m.rec, err
				}
			case <-tick.C:
			}
			continue
		}

		s.step++
		s.stagnation = 0
		m.Hub.set(func(snap *Snapshot) { snap.Step = s.step })

		assess := eng.Assess(reflex.Signals{
			Actions:     actionNorms,
			Errors:      errFPs,
			Tokens:      s.phaseCompl,
			TokenBurn:   m.Cfg.Reflex.Detectors.TokenBurn.Threshold,
			StagnationN: s.stagnation,
			Step:        s.step,
			Meaningful:  meaningful,
			ThinkChars:  s.thinkChars,
			ThinkOnly:   thinkOnly,
			ThinkText:   thinkText,
		})
		if len(actionNorms) > 0 || meaningful {
			s.thinkChars = 0
		}
		if thinkText != "" {
			eng.ObserveThink(thinkText)
		}
		for _, n := range actionNorms {
			eng.ObserveAction(n)
		}
		for _, fp := range errFPs {
			eng.ObserveError(fp)
		}
		assess, judgeUsed, _ := m.applyJudge(ctx, assess, s.phaseCompl, "")
		reasonKey := strings.Join(assess.Reasons, ",")
		if assess.State != lastState || reasonKey != lastReasons {
			emit(event.ProgressEvaluated, "reflex", map[string]any{
				"state": assess.State, "score": assess.Score, "reasons": assess.Reasons, "judge": judgeUsed,
			})
			lastState = assess.State
			lastReasons = reasonKey
		}
		m.Hub.set(func(snap *Snapshot) {
			snap.Reflex = assess
			snap.Progress = assess.Score
			snap.LastEvalSeq = m.seq
			snap.ReflexMode = m.mode()
			snap.JudgeOn = m.Cfg.Reflex.Judge.EffectiveEnabled()
		})

		switch assess.State {
		case reflex.Looping, reflex.Regressing, reflex.Stalled:
			if assess.State == reflex.Looping {
				emit(event.LoopDetected, "reflex", assess)
				m.Hub.set(func(snap *Snapshot) { snap.LastLoopSeq = m.seq })
			}
			if assess.State == reflex.Stalled {
				emit(event.StagnationDetected, "reflex", assess)
			}
			if assess.State == reflex.Regressing {
				emit(event.RegressionDetected, "reflex", assess)
			}
			if s.pending == nil {
				action, ok := s.ladder.NextFor(assess)
				if !ok {
					return m.fail(ctx, fmt.Errorf("recovery exhausted"))
				}
				if err := m.offerRecovery(ctx, action, assess, emit); err != nil {
					return m.rec, err
				}
			}
		}

		if m.shouldApplyPending() {
			if err := m.applyPending(ctx, emit); err != nil {
				return m.rec, err
			}
		}

		select {
		case <-ctx.Done():
			_ = h.Interrupt(ctx, h.SessionID())
			return m.cancel(ctx)
		case <-s.approve:
			if err := m.applyPending(ctx, emit); err != nil {
				return m.rec, err
			}
		case <-tick.C:
		}
	}
}

func sidecarUserOnly(ings []agent.Ingest) bool {
	hasUser := false
	for _, ing := range ings {
		switch ing.Type {
		case event.UserMessage:
			hasUser = true
		case event.ToolRequested, event.ToolCompleted, event.ModelThinking, event.ModelCompleted:
			return false
		}
	}
	return hasUser
}

func sidecarSignals(ings []agent.Ingest) (actions, errors []string, meaningful bool, thinkChars int, thinkOnly bool, thinkText string) {
	hasTool := false
	hasText := false
	for _, ing := range ings {
		switch ing.Type {
		case event.ToolRequested:
			hasTool = true
			actions = append(actions, reflex.NormalizeAction(fmt.Sprint(ing.Data["tool"]), fmt.Sprint(ing.Data["args"])))
		case event.ToolCompleted:
			if e, _ := ing.Data["error"].(string); e != "" {
				errors = append(errors, reflex.FingerprintError(e))
			}
			if !sidecarNoop(ing) {
				meaningful = true
			}
		case event.ModelThinking:
			thinkChars += ingestInt(ing.Data, "delta")
			if p := strings.TrimSpace(fmt.Sprint(ing.Data["preview"])); p != "" && p != "<nil>" {
				if thinkText != "" {
					thinkText += "\n"
				}
				thinkText += p
			}
		case event.ModelCompleted:
			hasText = true
			meaningful = true
		}
	}
	thinkOnly = thinkChars > 0 && !hasTool && !hasText && !meaningful
	return actions, errors, meaningful, thinkChars, thinkOnly, thinkText
}

func ingestInt(data map[string]any, key string) int {
	switch v := data[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func sidecarNoop(ing agent.Ingest) bool {
	if e, _ := ing.Data["error"].(string); e != "" {
		return true
	}
	tool := strings.ToLower(fmt.Sprint(ing.Data["tool"]))
	out := strings.ToLower(fmt.Sprint(ing.Data["output"]))
	if strings.Contains(out, "file unchanged") || strings.Contains(out, `"status": "unchanged"`) {
		return true
	}
	if strings.Contains(out, "blocked:") {
		return true
	}
	switch tool {
	case "tool_search", "tool_describe", "skills_list", "skill_view":
		return true
	}
	if discoveryMiss(out) {
		return true
	}
	return false
}

func discoveryMiss(out string) bool {
	if out == "" {
		return false
	}
	for _, p := range []string{
		"not available in this session",
		"not a deferrable tool",
		`"not_found"`,
		`"matches": []`,
		`"matches":[]`,
		"no cached tokens",
		"non-interactive environment",
		"failed to connect",
		"oauth error",
		"requires an interactive terminal",
	} {
		if strings.Contains(out, p) {
			return true
		}
	}
	return false
}

func systemNoise(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	return strings.HasPrefix(t, "[system:") || strings.HasPrefix(t, "[important:")
}

func (m *Manager) mode() string {
	if m.sess != nil && m.sess.mode != "" {
		return config.NormalizeReflexMode(m.sess.mode)
	}
	return m.Cfg.Reflex.EffectiveMode()
}

func (m *Manager) SetReflexMode(mode string) string {
	mode = config.NormalizeReflexMode(mode)
	m.Cfg.Reflex.Mode = mode
	if m.sess != nil {
		m.sess.mode = mode
		if mode == config.ReflexAuto && m.sess.pending != nil && m.sess.pending.Action != "human" {
			select {
			case m.sess.approve <- struct{}{}:
			default:
			}
		}
	}
	m.Hub.set(func(s *Snapshot) { s.ReflexMode = mode })
	if path := config.ProjectConfigFile(m.ConfigFiles); path != "" {
		_ = config.PersistReflexMode(path, mode)
	}
	return mode
}

func (m *Manager) ToggleReflexMode() string {
	next := config.ReflexAuto
	if m.mode() == config.ReflexAuto {
		next = config.ReflexHuman
	}
	return m.SetReflexMode(next)
}

func (m *Manager) ApproveRecovery() {
	if m.sess == nil {
		return
	}
	select {
	case m.sess.approve <- struct{}{}:
	default:
	}
}

func (m *Manager) offerRecovery(ctx context.Context, action string, assess reflex.Assessment, emit func(string, string, any)) error {
	if err := m.transition(ctx, StateRecovering); err != nil {
		return err
	}
	pending := m.mode() == config.ReflexHuman || action == "human"
	emit(event.RecoveryStarted, "reflex", map[string]any{"action": action, "pending": pending})
	m.debug("recovery", "action", action, "pending", pending, "state", assess.State)
	m.sess.pending = &pendingRec{Action: action, Assess: assess}
	m.Hub.set(func(s *Snapshot) {
		s.RecoveryRung = action
		s.PendingRecovery = action
		s.PendingReasons = append([]string(nil), assess.Reasons...)
		s.ReflexMode = m.mode()
	})
	if pending {
		_ = m.transition(ctx, StateWaitingHuman)
		return nil
	}
	return m.applyPending(ctx, emit)
}

func (m *Manager) shouldApplyPending() bool {
	if m.sess == nil || m.sess.pending == nil {
		return false
	}
	if m.sess.pending.Action == "human" || m.sess.pending.Held {
		return false
	}
	return m.mode() == config.ReflexAuto
}

func (m *Manager) waitApprove(ctx context.Context, tick func()) error {
	for {
		if m.sess == nil || m.sess.pending == nil {
			return nil
		}
		if m.shouldApplyPending() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.sess.approve:
			return nil
		case <-time.After(200 * time.Millisecond):
			if tick != nil {
				tick()
			}
		}
	}
}

func (m *Manager) applyPending(ctx context.Context, emit func(string, string, any)) error {
	if m.sess == nil || m.sess.pending == nil {
		return nil
	}
	p := m.sess.pending
	if err := m.transition(ctx, StateRecovering); err != nil {
		return err
	}
	prompt := recoveryPrompt(p.Action, m.sess.opts.Goal, p.Assess)
	switch p.Action {
	case "switch_model":
		fromProv, fromModel := m.rec.Provider, m.rec.Model
		switched, toProv, toModel := m.escalateModel(ctx, emit)
		if switched {
			prompt += fmt.Sprintf(" Temper switched from %s/%s to %s/%s. Continue with the new model.",
				fromProv, fromModel, toProv, toModel)
			emit(event.StrategyChanged, "reflex", map[string]any{
				"action": p.Action, "from": fromProv + "/" + fromModel, "to": toProv + "/" + toModel,
				"provider": toProv, "model": toModel,
			})
		} else {
			prompt += " Switch to a different model and retry."
			emit(event.StrategyChanged, "reflex", map[string]any{
				"action": p.Action, "from": fromProv + "/" + fromModel, "escalated": false,
			})
		}
	case "switch_agent":
		to, ok := pickHandoffAgent(m.Cfg, m.rec.Agent)
		if !ok {
			emit(event.RecoveryFailed, "reflex", map[string]any{"action": p.Action, "error": "no handoff target"})
			p.Held = true
			_ = m.transition(ctx, StateWaitingHuman)
			return nil
		}
		if err := m.switchSidecar(ctx, to, emit); err != nil {
			emit(event.RecoveryFailed, "reflex", map[string]any{"action": p.Action, "error": err.Error(), "to": to})
			p.Held = true
			p.LastErr = err.Error()
			_ = m.transition(ctx, StateWaitingHuman)
			return nil
		}
		emit(event.StrategyChanged, "reflex", map[string]any{"action": p.Action, "to": to})
		emit(event.RecoveryCompleted, "reflex", map[string]any{"action": p.Action, "to": to})
		m.clearPending()
		return m.transition(ctx, StateExecuting)
	case "human":
		emit(event.StrategyChanged, "reflex", map[string]any{"action": p.Action})
		emit(event.RecoveryCompleted, "reflex", map[string]any{"action": p.Action})
		m.clearPending()
		return nil
	}
	if m.sess.sidecar != nil {
		if err := m.sess.sidecar.Inject(ctx, prompt); err != nil {
			p.Held = true
			p.LastErr = err.Error()
			kind := "inject"
			if agent.IsLiveOwner(err) {
				kind = "live_owner"
			}
			if agent.IsInjectUnavailable(err) {
				kind = "inject_unavailable"
			}
			emit(event.RecoveryFailed, m.sess.sidecar.ID(), map[string]any{
				"action": p.Action, "error": err.Error(), "kind": kind,
			})
			m.debug("recovery.failed", "action", p.Action, "kind", kind, "err", err.Error())
			m.Hub.set(func(s *Snapshot) {
				s.PendingRecovery = p.Action
				s.PendingReasons = append([]string{kind}, p.Assess.Reasons...)
			})
			_ = m.transition(ctx, StateWaitingHuman)
			return nil
		}
	} else if m.sess.native != nil {
		m.sess.native.Inject("user", prompt)
	}
	if p.Action != "switch_model" {
		emit(event.StrategyChanged, "reflex", map[string]any{"action": p.Action})
	}
	emit(event.RecoveryCompleted, "reflex", map[string]any{"action": p.Action})
	m.clearPending()
	return m.transition(ctx, StateExecuting)
}

// escalateModel changes the active native provider/model, or records a model
// override for sidecars that advertise ModelOverride. Returns whether a real
// change was applied and the new provider/model ids.
func (m *Manager) escalateModel(ctx context.Context, emit func(string, string, any)) (bool, string, string) {
	if m.sess == nil {
		return false, m.rec.Provider, m.rec.Model
	}
	curProv, curModel := m.rec.Provider, m.rec.Model
	if m.sess.tried == nil {
		m.sess.tried = map[string]bool{}
	}
	if curProv != "" {
		m.sess.tried[curProv] = true
	}

	// Prefer another model on the same provider when native.
	if m.sess.native != nil && curProv != "" {
		if names, err := provider.ModelNames(ctx, m.Cfg, curProv); err == nil {
			for _, name := range names {
				if name != "" && name != curModel {
					m.sess.native.SetModel(name)
					m.rec.Model = name
					m.sess.opts.Model = name
					m.Hub.set(func(s *Snapshot) { s.Model = name })
					_ = m.Store.UpdateRun(ctx, m.rec)
					emit(event.RoutingDecided, "arbiter", map[string]any{
						"selected": map[string]string{"provider": curProv, "model": name},
						"reasons":  []string{"switch_model same-provider alternate"},
					})
					m.debug("switch_model", "provider", curProv, "from", curModel, "to", name)
					return true, curProv, name
				}
			}
		}
	}

	// Next usable provider (native).
	if m.sess.native != nil {
		if p, ok := m.fallbackProvider(ctx, m.sess.tried, emit); ok {
			m.sess.native.SetProvider(p, m.rec.Model)
			m.sess.opts.Model = m.rec.Model
			m.debug("switch_model", "provider", m.rec.Provider, "model", m.rec.Model, "via", "fallbackProvider")
			return true, m.rec.Provider, m.rec.Model
		}
	}

	// Sidecar with ModelOverride: pick a configured alternate and inject guidance.
	if m.sess.sidecar != nil {
		caps, _ := m.sess.sidecar.Capabilities(ctx)
		if caps.ModelOverride {
			alt := alternateSidecarModel(m.Cfg, m.sess.sidecar.ID(), curModel)
			if alt != "" && alt != curModel {
				m.rec.Model = alt
				m.sess.opts.Model = alt
				m.Hub.set(func(s *Snapshot) { s.Model = alt })
				_ = m.Store.UpdateRun(ctx, m.rec)
				emit(event.RoutingDecided, "arbiter", map[string]any{
					"selected": map[string]string{"agent": m.sess.sidecar.ID(), "model": alt},
					"reasons":  []string{"switch_model sidecar ModelOverride"},
				})
				m.debug("switch_model", "sidecar", m.sess.sidecar.ID(), "from", curModel, "to", alt)
				return true, m.rec.Provider, alt
			}
		}
	}
	return false, curProv, curModel
}

func alternateSidecarModel(cfg config.Config, agentID, current string) string {
	_ = agentID
	for _, cand := range []string{
		provider.DefaultModel(cfg, "anthropic"),
		provider.DefaultModel(cfg, "openai"),
		provider.DefaultModel(cfg, "ollama"),
		"gpt-4.1-mini", "claude-sonnet-4-5", "llama3.2",
	} {
		if cand != "" && cand != current {
			return cand
		}
	}
	return ""
}

func (m *Manager) clearPending() {
	if m.sess != nil {
		m.sess.pending = nil
	}
	m.Hub.set(func(s *Snapshot) {
		s.PendingRecovery = ""
		s.PendingReasons = nil
	})
}
