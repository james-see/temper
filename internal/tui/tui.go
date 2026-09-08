package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/run"
)

type phase int

const (
	phaseSplash phase = iota
	phaseInput
	phaseRunning
	phaseDone
)

type overlay int

const (
	overlayNone overlay = iota
	overlayHelp
	overlayStatus
	overlayQuit
)

type tickMsg time.Time

type Model struct {
	phase    phase
	overlay  overlay
	input    textinput.Model
	spin     spinner.Model
	vp       viewport.Model
	goal     string
	hub      *run.Hub
	cancel   context.CancelFunc
	inspect  bool
	width    int
	height   int
	start    time.Time
	ready    bool
	err      error
	onStart  func(goal string)
}

func New(goal string, hub *run.Hub, cancel context.CancelFunc, inspect bool, onStart func(string)) Model {
	ti := textinput.New()
	ti.Placeholder = "what should temper do?"
	ti.CharLimit = 500
	ti.Width = 60
	ti.Focus()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinStyle

	m := Model{
		phase:   phaseSplash,
		input:   ti,
		spin:    s,
		goal:    goal,
		hub:     hub,
		cancel:  cancel,
		inspect: inspect,
		width:   80,
		height:  24,
		start:   time.Now(),
		onStart: onStart,
	}
	if inspect {
		m.phase = phaseDone
	}
	return m
}

func (m Model) Init() tea.Cmd {
	if m.inspect {
		return tea.Batch(m.spin.Tick, tickEvery())
	}
	return tea.Batch(m.spin.Tick, splashWait(), tickEvery())
}

type splashDone struct{}

func splashWait() tea.Cmd {
	return tea.Tick(700*time.Millisecond, func(time.Time) tea.Msg { return splashDone{} })
}

func tickEvery() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerH, footerH := 3, 4
		h := max(3, m.height-headerH-footerH)
		if !m.ready {
			m.vp = viewport.New(max(20, m.width-2), h)
			m.vp.Style = vpStyle
			m.ready = true
		} else {
			m.vp.Width = max(20, m.width-2)
			m.vp.Height = h
		}
		m.input.Width = max(20, m.width-8)
		return m, nil

	case splashDone:
		if m.inspect {
			m.phase = phaseDone
			return m, nil
		}
		if m.goal == "" {
			m.phase = phaseInput
			return m, textinput.Blink
		}
		m.phase = phaseRunning
		if m.onStart != nil {
			m.onStart(m.goal)
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tickMsg:
		if m.hub != nil && m.ready && (m.phase == phaseRunning || m.phase == phaseDone || m.inspect) {
			snap := m.hub.Get()
			m.vp.SetContent(renderEvents(snap.Events, m.vp.Width))
			if snap.Done && m.phase == phaseRunning {
				m.phase = phaseDone
			}
		}
		return m, tickEvery()

	case tea.KeyMsg:
		if m.overlay == overlayQuit {
			switch msg.String() {
			case "y", "Y":
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Quit
			case "n", "N", "esc":
				m.overlay = overlayNone
				return m, nil
			}
		}
		if m.overlay != overlayNone && m.overlay != overlayQuit {
			switch msg.String() {
			case "esc", "?", "s":
				m.overlay = overlayNone
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+c":
			if m.cancel != nil {
				m.cancel()
			}
			if m.phase == phaseRunning {
				return m, nil
			}
			return m, tea.Quit
		case "q":
			if m.phase == phaseRunning && !m.inspect {
				m.overlay = overlayQuit
				return m, nil
			}
			return m, tea.Quit
		case "esc":
			if m.overlay != overlayNone {
				m.overlay = overlayNone
				return m, nil
			}
			if m.phase == phaseRunning && !m.inspect {
				m.overlay = overlayQuit
				return m, nil
			}
			return m, tea.Quit
		case "?":
			if m.overlay == overlayHelp {
				m.overlay = overlayNone
			} else {
				m.overlay = overlayHelp
			}
			return m, nil
		case "s":
			if m.phase != phaseInput {
				if m.overlay == overlayStatus {
					m.overlay = overlayNone
				} else {
					m.overlay = overlayStatus
				}
			}
			return m, nil
		case "e":
			if m.hub != nil && m.ready {
				snap := m.hub.Get()
				seq := snap.LastEvalSeq
				if snap.LastLoopSeq > seq {
					seq = snap.LastLoopSeq
				}
				if seq > 0 {
					m.vp.SetYOffset(int(seq) - 1)
				}
			}
			return m, nil
		case "g":
			m.vp.GotoTop()
			return m, nil
		case "G":
			m.vp.GotoBottom()
			return m, nil
		case "j", "down":
			m.vp.LineDown(1)
			return m, nil
		case "k", "up":
			m.vp.LineUp(1)
			return m, nil
		}

		if m.phase == phaseInput {
			if msg.String() == "enter" {
				g := strings.TrimSpace(m.input.Value())
				if g != "" {
					m.goal = g
					m.phase = phaseRunning
					if m.onStart != nil {
						m.onStart(g)
					}
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.phase == phaseSplash {
		return m.viewSplash()
	}
	if m.phase == phaseInput {
		return m.viewInput()
	}
	body := m.viewRun()
	switch m.overlay {
	case overlayHelp:
		return stack(body, helpOverlay(m.width))
	case overlayStatus:
		return stack(body, m.statusOverlay())
	case overlayQuit:
		return stack(body, quitOverlay(m.width))
	}
	return body
}

func (m Model) viewSplash() string {
	var b strings.Builder
	b.WriteString(wordmarkStyle.Render(wordmark))
	b.WriteString("\n")
	b.WriteString(m.spin.View())
	b.WriteString(dimStyle.Render(" starting runtime..."))
	return b.String()
}

func (m Model) viewInput() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("TEMPER"))
	b.WriteString(dimStyle.Render("  ·  new run"))
	b.WriteString("\n\n")
	b.WriteString(boxStyle.Render(m.input.View()))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("enter to start  ·  ctrl+c quit  ·  ? help"))
	return b.String()
}

func (m Model) viewRun() string {
	snap := Snapshot{}
	if m.hub != nil {
		snap = snapFrom(m.hub.Get())
	}
	if snap.Started.IsZero() {
		snap.Started = m.start
	}
	header := fmt.Sprintf("TEMPER  ·  %s  ·  %s  ·  %s  ·  %s  ·  %s",
		short(snap.RunID, 14), snap.State, nz(snap.Agent, "native"), nz(snap.Provider, "—"), nz(snap.Model, "—"))
	var b strings.Builder
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")
	if m.ready {
		b.WriteString(m.vp.View())
	}
	b.WriteString("\n")
	b.WriteString(m.footer(snap))
	return b.String()
}

func (m Model) footer(snap Snapshot) string {
	pct := int(snap.Progress * 100)
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	bar := progressBar(32, pct)
	elapsed := time.Since(snap.Started).Truncate(time.Second)
	ref := reflexStyle(string(snap.Reflex.State)).Render(string(snap.Reflex.State))
	if snap.Reflex.State == "" {
		ref = dimStyle.Render("—")
	}
	spin := m.spin.View()
	if snap.Done {
		spin = "•"
	}
	return fmt.Sprintf("%s %s  %d%%  %s  reflex %s  tok %d/%d  ·  ?",
		spin, bar, pct, elapsed, ref, snap.TokensWorker, snap.TokensJudge)
}

func (m Model) statusOverlay() string {
	snap := Snapshot{}
	if m.hub != nil {
		snap = snapFrom(m.hub.Get())
	}
	judge := "off"
	if snap.JudgeOn {
		judge = "on"
	}
	body := fmt.Sprintf(`run        %s
state      %s
agent      %s
provider   %s
model      %s
workspace  %s
budget     %.4f / %.2f
tokens     worker %d  judge %d
reflex     %s  %s
recovery   %s
judge      %s`,
		snap.RunID, snap.State, snap.Agent, snap.Provider, snap.Model, snap.Workspace,
		snap.BudgetUsed, snap.BudgetMax, snap.TokensWorker, snap.TokensJudge,
		snap.Reflex.State, strings.Join(snap.Reflex.Reasons, ", "),
		nz(snap.RecoveryRung, "—"), judge)
	return overlayBox("status", body, m.width)
}

func helpOverlay(width int) string {
	return overlayBox("keys", `?     help
s     status
e     last progress.evaluated / loop.detected
j/k   scroll
g/G   top / bottom
^C    cancel
q     quit
esc   close overlay / quit prompt`, width)
}

func quitOverlay(width int) string {
	return overlayBox("quit", "cancel run and quit?  y / n", width)
}

type Snapshot = run.Snapshot

func snapFrom(s run.Snapshot) Snapshot { return s }

func renderEvents(evs []event.Event, width int) string {
	if len(evs) == 0 {
		return dimStyle.Render("waiting for events…")
	}
	var b strings.Builder
	for _, ev := range evs {
		line := fmt.Sprintf("%-4d  %s  %s", ev.Sequence, ev.Type, clipData(ev))
		if width > 8 && len(line) > width-2 {
			line = line[:width-2]
		}
		b.WriteString(eventStyle(ev.Type).Render(line))
		b.WriteString("\n")
	}
	return b.String()
}

func clipData(ev event.Event) string {
	s := strings.TrimSpace(string(ev.Data))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

func progressBar(width, pct int) string {
	if width < 4 {
		width = 4
	}
	filled := (pct * width) / 100
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func overlayBox(title, body string, width int) string {
	w := min(72, max(40, width-6))
	return "\n" + boxStyle.Width(w).Render(titleStyle.Render(title)+"\n"+body)
}

func stack(base, over string) string {
	return base + "\n" + over
}

func short(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func nz(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Run(goal string, hub *run.Hub, cancel context.CancelFunc, inspect bool, onStart func(string)) error {
	p := tea.NewProgram(New(goal, hub, cancel, inspect, onStart), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
