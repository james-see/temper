package run

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/james-see/temper/internal/agent"
	"github.com/james-see/temper/internal/arbiter"
	"github.com/james-see/temper/internal/artifact"
	"github.com/james-see/temper/internal/config"
	"github.com/james-see/temper/internal/evaluator"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/provider"
	"github.com/james-see/temper/internal/reflex"
	"github.com/james-see/temper/internal/store"
	"github.com/james-see/temper/internal/workspace"
)

const (
	StateCreated    = "created"
	StateClassified = "classified"
	StatePlanned    = "planned"
	StateExecuting  = "executing"
	StateEvaluating = "evaluating"
	StateRecovering = "recovering"
	StateCompleted  = "completed"
	StateFailed     = "failed"
	StateCancelled  = "cancelled"
)

type Snapshot struct {
	RunID         string
	Goal          string
	State         string
	Agent         string
	Provider      string
	Model         string
	Workspace     string
	Events        []event.Event
	Progress      float64
	Step          int
	Started       time.Time
	Reflex        reflex.Assessment
	RecoveryRung  string
	JudgeOn       bool
	JudgeUsed     bool
	TokensWorker  int
	TokensJudge   int
	BudgetUsed    float64
	BudgetMax     float64
	LastEvalSeq   uint64
	LastLoopSeq   uint64
	Done          bool
	Err           string
}

type Hub struct {
	mu   sync.RWMutex
	snap Snapshot
}

func (h *Hub) Get() Snapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	s := h.snap
	s.Events = append([]event.Event(nil), h.snap.Events...)
	return s
}

func (h *Hub) set(fn func(*Snapshot)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fn(&h.snap)
}

type Options struct {
	Goal     string
	Root     string
	Agent    string
	Provider string
	Model    string
	Plain    bool
	Prov     provider.Provider
	MaxSteps int
}

type Manager struct {
	Cfg   config.Config
	Store *store.Store
	Arts  *artifact.Store
	Hub   *Hub
	Log   *slog.Logger
	seq   uint64
	rec   store.Run
}

func Open(cfg config.Config, root string) (*Manager, error) {
	data := config.DataDir(root)
	if err := os.MkdirAll(filepath.Join(data, "artifacts"), 0o755); err != nil {
		return nil, err
	}
	st, err := store.Open(filepath.Join(data, "temper.db"))
	if err != nil {
		return nil, err
	}
	arts, err := artifact.Open(filepath.Join(data, "artifacts"))
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	return &Manager{
		Cfg:   cfg,
		Store: st,
		Arts:  arts,
		Hub:   &Hub{},
		Log:   slog.Default(),
	}, nil
}

func (m *Manager) Close() error {
	if m.Store != nil {
		return m.Store.Close()
	}
	return nil
}

func (m *Manager) Execute(ctx context.Context, opts Options) (store.Run, error) {
	if opts.MaxSteps <= 0 {
		opts.MaxSteps = 24
	}
	id := newID()
	ws, err := workspace.New(opts.Root, config.DataDir(opts.Root))
	if err != nil {
		return store.Run{}, err
	}
	if err := ws.Prepare(id); err != nil {
		return store.Run{}, err
	}
	dec := arbiter.Select(m.Cfg, opts.Agent, opts.Provider, opts.Model)
	m.rec = store.Run{
		ID:        id,
		Goal:      opts.Goal,
		State:     StateCreated,
		Agent:     dec.Selected.Agent,
		Provider:  dec.Selected.Provider,
		Model:     dec.Selected.Model,
		Workspace: ws.Root(),
		CreatedAt: time.Now().UTC(),
	}
	if err := m.Store.CreateRun(ctx, m.rec); err != nil {
		return store.Run{}, err
	}
	m.Hub.set(func(s *Snapshot) {
		*s = Snapshot{
			RunID: id, Goal: opts.Goal, State: StateCreated,
			Agent: m.rec.Agent, Provider: m.rec.Provider, Model: m.rec.Model,
			Workspace: m.rec.Workspace, Started: time.Now(),
			BudgetMax: m.Cfg.Temper.Budget.MaxCostPerTask,
			JudgeOn:   m.Cfg.Reflex.Judge.Model != "",
		}
	})

	emit := func(typ, actor string, data any) {
		m.emit(ctx, typ, actor, data)
	}

	emit(event.RunCreated, "runtime", map[string]any{"goal": opts.Goal})
	emit(event.RunStarted, "runtime", map[string]any{"workspace": ws.Root()})
	if err := m.transition(ctx, StateClassified); err != nil {
		return m.rec, err
	}
	emit(event.TaskClassified, "runtime", map[string]any{"goal": opts.Goal})
	emit(event.RoutingDecided, "arbiter", dec)
	emit(event.AgentSelected, "arbiter", dec.Selected)

	prov := opts.Prov
	if prov == nil {
		var err error
		prov, _, err = provider.Resolve(m.Cfg, m.rec.Provider)
		if err != nil {
			return m.fail(ctx, err)
		}
	}
	native := agent.NewNative(prov, m.rec.Model, ws.Root(), m.Cfg.Shell.Timeout, m.Cfg.Shell.Deny)
	if _, err := native.Start(ctx, agent.TaskRequest{RunID: id, Prompt: opts.Goal, Workspace: ws.Root(), Model: m.rec.Model}); err != nil {
		return m.fail(ctx, err)
	}
	if err := m.transition(ctx, StatePlanned); err != nil {
		return m.rec, err
	}
	emit(event.PlanCreated, "native", map[string]any{"plan": "execute goal with workspace tools"})
	emit(event.AgentStarted, "native", map[string]any{"id": "native"})

	eng := reflex.NewEngine(
		m.Cfg.Reflex.Detectors.ActionCycle.Repetitions,
		m.Cfg.Reflex.Detectors.RepeatedError.Threshold,
		m.Cfg.Reflex.Detectors.TokenBurn.Threshold,
		m.Cfg.Reflex.Detectors.Stagnation.Actions,
		m.Cfg.Reflex.Detectors.Regression.Enabled,
	)
	actions := recoveryActions(m.Cfg)
	ladder := reflex.NewLadder(actions)
	eval := &evaluator.Engine{
		Workdir: ws.Root(),
		Test:    m.Cfg.Evaluator.Test,
		Compile: m.Cfg.Evaluator.Compile,
		Timeout: m.Cfg.Shell.Timeout,
	}

	var prevPass *bool
	var workerTok, judgeTok int
	stagnation := 0

	for step := 1; step <= opts.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return m.cancel(ctx)
		}
		if err := m.transition(ctx, StateExecuting); err != nil {
			return m.rec, err
		}
		m.Hub.set(func(s *Snapshot) { s.Step = step })

		res := native.Step(ctx)
		workerTok += res.Usage.PromptTokens + res.Usage.CompletionTokens
		m.Hub.set(func(s *Snapshot) {
			s.TokensWorker = workerTok
			s.BudgetUsed = costOf(workerTok, judgeTok)
		})
		if res.Err != nil {
			emit(event.ModelCompleted, "provider", map[string]any{"error": res.Err.Error()})
			return m.fail(ctx, res.Err)
		}
		emit(event.ModelCalled, "native", map[string]any{"model": m.rec.Model, "provider": m.rec.Provider})
		emit(event.ModelCompleted, "native", map[string]any{
			"tokens": res.Usage, "tool_calls": len(res.ToolCalls),
		})

		var actionNorms []string
		var errFPs []string
		for _, tc := range res.ToolCalls {
			emit(event.ToolRequested, "native", map[string]any{"tool": tc.Name, "args": clip(tc.Arguments, 400)})
			eng.ObserveAction(reflex.NormalizeAction(tc.Name, tc.Arguments))
			actionNorms = append(actionNorms, reflex.NormalizeAction(tc.Name, tc.Arguments))
		}
		for _, out := range res.ToolOut {
			payload := map[string]any{"tool": out.Name, "ms": out.Duration.Milliseconds()}
			if out.Err != "" {
				payload["error"] = out.Err
				fp := reflex.FingerprintError(out.Err)
				eng.ObserveError(fp)
				errFPs = append(errFPs, fp)
			} else {
				payload["output"] = clip(out.Output, 400)
			}
			emit(event.ToolCompleted, "native", payload)
			for _, f := range out.Files {
				emit(event.FileModified, "native", map[string]any{"path": f})
			}
		}

		if err := m.transition(ctx, StateEvaluating); err != nil {
			return m.rec, err
		}
		var evals []evaluator.Result
		if eval.Test != "" || eval.Compile != "" {
			evals = eval.RunAll(ctx)
			for _, ev := range evals {
				id, _ := m.Arts.Put([]byte(ev.Output))
				kind := event.TestExecuted
				if ev.Kind == "compile" {
					kind = event.CompileExecuted
				}
				emit(kind, "evaluator", map[string]any{
					"command": ev.Command, "passed": ev.Passed, "artifact": id, "ms": ev.Duration.Milliseconds(),
				})
			}
			emit(event.EvaluationCompleted, "evaluator", map[string]any{"passed": evaluator.AllPassed(evals)})
		}

		diff, _ := ws.Status()
		meaningful := strings.TrimSpace(diff) != ""
		if !meaningful {
			stagnation++
		} else {
			stagnation = 0
			if hash, err := ws.Checkpoint(fmt.Sprintf("temper %s step %d", id, step)); err == nil && hash != "" {
				emit(event.CheckpointCreated, "workspace", map[string]any{"hash": hash})
			}
		}
		var passed *bool
		if len(evals) > 0 {
			p := evaluator.AllPassed(evals)
			passed = &p
		}
		assess := eng.Assess(reflex.Signals{
			Actions:      actionNorms,
			Errors:       errFPs,
			Tokens:       workerTok,
			TokenBurn:    m.Cfg.Reflex.Detectors.TokenBurn.Threshold,
			EvalPassed:   passed,
			PrevEvalPass: prevPass,
			Meaningful:   meaningful,
			StagnationN:  stagnation,
		})
		if passed != nil {
			prevPass = passed
		}

		judgeUsed := false
		if assess.State == reflex.Uncertain && m.Cfg.Reflex.Judge.Model != "" {
			jr := reflex.InvokeJudge(ctx, m.Cfg.Reflex.Judge.Endpoint, m.Cfg.Reflex.Judge.Model, reflex.JudgeInput{
				Events:   m.eventDigest(),
				Tokens:   workerTok,
				Cost:     costOf(workerTok, judgeTok),
				EvalNote: fmt.Sprintf("%v", passed),
			})
			judgeTok += jr.Usage.PromptTokens + jr.Usage.CompletionTokens
			m.Hub.set(func(s *Snapshot) { s.TokensJudge = judgeTok; s.JudgeUsed = jr.Used })
			if jr.Used && jr.Err == nil {
				assess = jr.Assessment
				if len(assess.Reasons) == 0 {
					assess.Reasons = []string{"judge"}
				}
				judgeUsed = true
			}
		}

		emit(event.ProgressEvaluated, "reflex", map[string]any{
			"state": assess.State, "score": assess.Score, "reasons": assess.Reasons, "judge": judgeUsed,
		})
		m.Hub.set(func(s *Snapshot) {
			s.Reflex = assess
			s.Progress = assess.Score
			s.LastEvalSeq = m.seq
			s.TokensJudge = judgeTok
		})

		switch assess.State {
		case reflex.Complete:
			return m.complete(ctx)
		case reflex.Looping, reflex.Regressing, reflex.Stalled:
			if assess.State == reflex.Looping {
				emit(event.LoopDetected, "reflex", assess)
				m.Hub.set(func(s *Snapshot) { s.LastLoopSeq = m.seq })
			}
			if assess.State == reflex.Stalled {
				emit(event.StagnationDetected, "reflex", assess)
			}
			if assess.State == reflex.Regressing {
				emit(event.RegressionDetected, "reflex", assess)
			}
			action, ok := ladder.Next()
			if !ok {
				return m.fail(ctx, fmt.Errorf("recovery exhausted"))
			}
			if err := m.transition(ctx, StateRecovering); err != nil {
				return m.rec, err
			}
			emit(event.RecoveryStarted, "reflex", map[string]any{"action": action})
			m.Hub.set(func(s *Snapshot) { s.RecoveryRung = action })
			native.Inject("user", recoveryPrompt(action, opts.Goal, assess))
			emit(event.StrategyChanged, "reflex", map[string]any{"action": action})
			emit(event.RecoveryCompleted, "reflex", map[string]any{"action": action})
		}

		if costOf(workerTok, judgeTok) >= m.Cfg.Temper.Budget.MaxCostPerTask && m.Cfg.Temper.Budget.MaxCostPerTask > 0 {
			return m.fail(ctx, fmt.Errorf("budget exceeded"))
		}
		if res.Done && assess.State == reflex.Progressing && eval.Test == "" && eval.Compile == "" {
			return m.complete(ctx)
		}
	}
	return m.fail(ctx, fmt.Errorf("max steps exceeded"))
}

func (m *Manager) Load(ctx context.Context, id string) (store.Run, []event.Event, error) {
	r, err := m.Store.GetRun(ctx, id)
	if err != nil {
		return store.Run{}, nil, err
	}
	evs, err := m.Store.ListEvents(ctx, id)
	return r, evs, err
}

func (m *Manager) Hydrate(r store.Run, evs []event.Event) {
	s := Snapshot{
		RunID: r.ID, Goal: r.Goal, State: r.State, Agent: r.Agent, Provider: r.Provider, Model: r.Model,
		Workspace: r.Workspace, Events: evs, Started: r.CreatedAt, Done: terminal(r.State),
		BudgetMax: m.Cfg.Temper.Budget.MaxCostPerTask,
		JudgeOn:   m.Cfg.Reflex.Judge.Model != "",
	}
	for _, ev := range evs {
		switch ev.Type {
		case event.ProgressEvaluated:
			s.LastEvalSeq = ev.Sequence
			var a reflex.Assessment
			_ = json.Unmarshal(ev.Data, &a)
			if a.State != "" {
				s.Reflex = a
				s.Progress = a.Score
			}
		case event.LoopDetected:
			s.LastLoopSeq = ev.Sequence
		case event.RecoveryStarted:
			var d struct {
				Action string `json:"action"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			s.RecoveryRung = d.Action
		case event.ModelCompleted:
			var d struct {
				Tokens provider.Usage `json:"tokens"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			s.TokensWorker += d.Tokens.PromptTokens + d.Tokens.CompletionTokens
		}
	}
	s.BudgetUsed = costOf(s.TokensWorker, s.TokensJudge)
	m.Hub.set(func(cur *Snapshot) { *cur = s })
}

func (m *Manager) emit(ctx context.Context, typ, actor string, data any) {
	m.seq++
	ev := event.Event{
		ID:        fmt.Sprintf("%s-%d", m.rec.ID, m.seq),
		RunID:     m.rec.ID,
		Sequence:  m.seq,
		Type:      typ,
		Timestamp: time.Now().UTC(),
		Actor:     actor,
		Data:      store.Encode(data),
	}
	if err := m.Store.AppendEvent(ctx, ev); err != nil {
		m.Log.Error("append event", "err", err, "type", typ)
	}
	m.Hub.set(func(s *Snapshot) {
		s.Events = append(s.Events, ev)
		s.State = m.rec.State
	})
}

func (m *Manager) transition(ctx context.Context, state string) error {
	m.rec.State = state
	m.Hub.set(func(s *Snapshot) { s.State = state })
	return m.Store.UpdateRun(ctx, m.rec)
}

func (m *Manager) complete(ctx context.Context) (store.Run, error) {
	m.emit(ctx, event.RunCompleted, "runtime", map[string]any{"state": StateCompleted})
	_ = m.transition(ctx, StateCompleted)
	m.Hub.set(func(s *Snapshot) { s.Done = true; s.Progress = 1 })
	return m.rec, nil
}

func (m *Manager) fail(ctx context.Context, err error) (store.Run, error) {
	m.emit(ctx, event.RunFailed, "runtime", map[string]any{"error": err.Error()})
	_ = m.transition(ctx, StateFailed)
	m.Hub.set(func(s *Snapshot) { s.Done = true; s.Err = err.Error() })
	return m.rec, err
}

func (m *Manager) cancel(ctx context.Context) (store.Run, error) {
	m.emit(ctx, event.RunCancelled, "runtime", map[string]any{})
	_ = m.transition(ctx, StateCancelled)
	m.Hub.set(func(s *Snapshot) { s.Done = true; s.Err = "cancelled" })
	return m.rec, context.Canceled
}

func (m *Manager) eventDigest() string {
	snap := m.Hub.Get()
	var b strings.Builder
	start := 0
	if len(snap.Events) > 40 {
		start = len(snap.Events) - 40
	}
	for _, ev := range snap.Events[start:] {
		fmt.Fprintf(&b, "%d %s %s\n", ev.Sequence, ev.Type, clip(string(ev.Data), 120))
	}
	return b.String()
}

func recoveryActions(cfg config.Config) []string {
	var out []string
	for _, s := range cfg.Reflex.Recovery {
		out = append(out, s.Action)
	}
	return out
}

func recoveryPrompt(action, goal string, a reflex.Assessment) string {
	return fmt.Sprintf("Reflex %s (%s). Reasons: %s. Action: %s. Re-anchor on goal: %s",
		a.State, strings.Join(a.Reasons, ", "), strings.Join(a.Reasons, ", "), action, goal)
}

func costOf(worker, judge int) float64 {
	return float64(worker+judge) * 0.000002
}

func terminal(state string) bool {
	return state == StateCompleted || state == StateFailed || state == StateCancelled
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func newID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
