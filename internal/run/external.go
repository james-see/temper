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

	h := opts.Hermes
	if h == nil {
		cmd := "hermes"
		if a, ok := m.Cfg.Agents[dec.Selected.Agent]; ok && a.Command != "" {
			cmd = a.Command
		} else if a, ok := m.Cfg.Agents["hermes"]; ok && a.Command != "" {
			cmd = a.Command
		}
		h = agent.NewHermes(cmd, !opts.SkipSpawn)
	} else if opts.SkipSpawn {
		h.Spawn = false
	}

	if _, err := h.Start(ctx, agent.TaskRequest{
		RunID: id, Prompt: opts.Goal, Workspace: ws.Root(),
	}); err != nil {
		if strings.Contains(err.Error(), "PATH") {
			return m.fail(ctx, err)
		}
		m.debug("hermes.term", "err", err.Error(), "cmd", h.LaunchLine())
	}

	if err := m.transition(ctx, StatePlanned); err != nil {
		return m.rec, err
	}
	emit := func(typ, actor string, data any) { m.emit(ctx, typ, actor, data) }
	emit(event.PlanCreated, "hermes", map[string]any{"plan": "sidecar control plane"})
	emit(event.AgentStarted, "hermes", map[string]any{
		"id": "hermes", "command": h.LaunchLine(), "spawn": !opts.SkipSpawn,
	})
	if opts.SkipSpawn {
		m.debug("hermes.plain", "cmd", h.LaunchLine())
	}

	eng := reflex.NewEngine(
		m.Cfg.Reflex.Detectors.ActionCycle.Repetitions,
		m.Cfg.Reflex.Detectors.RepeatedError.Threshold,
		m.Cfg.Reflex.Detectors.TokenBurn.Threshold,
		m.Cfg.Reflex.Detectors.Stagnation.Actions,
		m.Cfg.Reflex.Detectors.Regression.Enabled,
	)
	m.sess = &session{
		hermes:  h,
		ws:      ws,
		opts:    opts,
		tried:   tried,
		eng:     eng,
		ladder:  reflex.NewLadder(sidecarRecovery(m.Cfg)),
		approve: make(chan struct{}, 1),
		mode:    m.Cfg.Reflex.EffectiveMode(),
	}
	m.debug("routing", "agent", m.rec.Agent, "sidecar", true, "spawn", !opts.SkipSpawn)
	return m.driveExternal(ctx)
}

func (m *Manager) driveExternal(ctx context.Context) (store.Run, error) {
	s := m.sess
	h := s.hermes
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
			m.debug("hermes.poll", "err", pollErr.Error())
		}
		userOnly := sidecarUserOnly(ings)
		goalShift := false
		for i, ing := range ings {
			if ing.Type == event.UserMessage {
				text := strings.TrimSpace(fmt.Sprint(ing.Data["text"]))
				kind := classifyTurn(s.opts.Goal, text)
				if ing.Data == nil {
					ing.Data = map[string]any{}
				}
				ing.Data["kind"] = kind
				ing.Data["same_goal"] = kind != turnNew
				ings[i] = ing
				if m.adoptUserGoal(ctx, text, kind) {
					goalShift = true
				}
				eng.Reset()
				s.stagnation = 0
				s.thinkChars = 0
				s.step = 0
				s.phaseCompl = 0
				s.phasePrompt = 0
			}
			emit(ing.Type, "hermes", ing.Data)
		}
		if goalShift {
			lastState = ""
			lastReasons = ""
		}
		actionNorms, errFPs, meaningful, thinkDelta, _, thinkText := sidecarSignals(ings)
		s.thinkChars += thinkDelta
		thinkOnly := s.thinkChars > 0 && len(actionNorms) == 0 && !meaningful
		if id := h.SessionID(); id != "" {
			m.Hub.set(func(snap *Snapshot) { snap.SessionID = id; snap.Attach = "session+logs" })
			if !bound {
				bound = true
				emit(event.AgentStarted, "hermes", map[string]any{"id": "hermes", "session": id})
			}
		}

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
				action, ok := s.ladder.Next()
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
	out := strings.ToLower(fmt.Sprint(ing.Data["output"]))
	if strings.Contains(out, "file unchanged") || strings.Contains(out, `"status": "unchanged"`) {
		return true
	}
	if strings.Contains(out, "blocked:") {
		return true
	}
	return false
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
		prompt += " Switch to a different model and retry."
	case "switch_agent":
		emit(event.AgentSwitched, "reflex", map[string]any{"from": m.rec.Agent, "to": ""})
		emit(event.StrategyChanged, "reflex", map[string]any{"action": p.Action})
		emit(event.RecoveryCompleted, "reflex", map[string]any{"action": p.Action})
		m.clearPending()
		return nil
	case "human":
		emit(event.StrategyChanged, "reflex", map[string]any{"action": p.Action})
		emit(event.RecoveryCompleted, "reflex", map[string]any{"action": p.Action})
		m.clearPending()
		return nil
	}
	if m.sess.hermes != nil {
		if err := m.sess.hermes.Inject(ctx, prompt); err != nil {
			p.Held = true
			p.LastErr = err.Error()
			kind := "inject"
			if agent.IsLiveOwner(err) {
				kind = "live_owner"
			}
			emit(event.RecoveryFailed, "hermes", map[string]any{
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
	emit(event.StrategyChanged, "reflex", map[string]any{"action": p.Action})
	emit(event.RecoveryCompleted, "reflex", map[string]any{"action": p.Action})
	m.clearPending()
	return m.transition(ctx, StateExecuting)
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
