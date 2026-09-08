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
		if opts.SkipSpawn {
			m.debug("hermes.spawn_skipped", "err", err.Error(), "cmd", h.LaunchLine())
		} else {
			m.emit(ctx, event.AgentStarted, "hermes", map[string]any{
				"id": "hermes", "error": err.Error(), "command": h.LaunchLine(),
			})
			// keep going if open-term failed: operator can run the printed command
			if !strings.Contains(err.Error(), "PATH") && !strings.Contains(err.Error(), "--version") {
				m.debug("hermes.term", "err", err.Error(), "cmd", h.LaunchLine())
			} else {
				return m.fail(ctx, err)
			}
		}
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
		ladder:  reflex.NewLadder(recoveryActions(m.Cfg)),
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
	idle := 0
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

	for {
		if err := ctx.Err(); err != nil {
			_ = h.Interrupt(ctx, h.SessionID())
			return m.cancel(ctx)
		}
		if err := m.transition(ctx, StateExecuting); err != nil {
			return m.rec, err
		}
		s.step++
		m.Hub.set(func(snap *Snapshot) { snap.Step = s.step })

		ings, _ := h.Poll(ctx)
		var actionNorms []string
		var errFPs []string
		for _, ing := range ings {
			emit(ing.Type, "hermes", ing.Data)
			switch ing.Type {
			case event.ToolRequested:
				tool := fmt.Sprint(ing.Data["tool"])
				args := fmt.Sprint(ing.Data["args"])
				actionNorms = append(actionNorms, reflex.NormalizeAction(tool, args))
			case event.ToolCompleted:
				if e, _ := ing.Data["error"].(string); e != "" {
					errFPs = append(errFPs, reflex.FingerprintError(e))
				}
			}
		}
		if id, ok := h.Discover(ctx); ok && !bound {
			bound = true
			m.Hub.set(func(snap *Snapshot) { snap.SessionID = id; snap.Attach = "session+logs" })
			emit(event.AgentStarted, "hermes", map[string]any{"id": "hermes", "session": id})
		} else if id := h.SessionID(); id != "" {
			m.Hub.set(func(snap *Snapshot) { snap.SessionID = id })
		}

		if len(ings) == 0 {
			idle++
		} else {
			idle = 0
			s.stagnation = 0
		}
		if idle > 0 && len(actionNorms) == 0 && len(errFPs) == 0 {
			s.stagnation++
		}

		assess := eng.Assess(reflex.Signals{
			Actions:     actionNorms,
			Errors:      errFPs,
			Tokens:      s.phaseCompl,
			TokenBurn:   m.Cfg.Reflex.Detectors.TokenBurn.Threshold,
			StagnationN: s.stagnation,
			Step:        s.step,
			Meaningful:  len(ings) > 0,
		})
		for _, n := range actionNorms {
			eng.ObserveAction(n)
		}
		for _, fp := range errFPs {
			eng.ObserveError(fp)
		}
		emit(event.ProgressEvaluated, "reflex", map[string]any{
			"state": assess.State, "score": assess.Score, "reasons": assess.Reasons,
		})
		m.Hub.set(func(snap *Snapshot) {
			snap.Reflex = assess
			snap.Progress = assess.Score
			snap.LastEvalSeq = m.seq
			snap.ReflexMode = m.mode()
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
	if m.sess.pending.Action == "human" {
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
			emit(event.RecoveryFailed, "hermes", map[string]any{"action": p.Action, "error": err.Error()})
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
