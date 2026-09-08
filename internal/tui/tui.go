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
	"github.com/james-see/temper/internal/provider"
	"github.com/james-see/temper/internal/run"
)

type phase int

const (
	phaseSplash phase = iota
	phasePickProvider
	phasePickModel
	phaseModelInput
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

type catalogMsg struct{}

type modelsMsg struct {
	Models []string
	Err    error
}

type pickItem struct {
	ID     string
	Title  string
	Detail string
	Usable bool
}

type StartFn func(goal, provider, model string)

type Options struct {
	Goal       string
	Provider   string
	Model      string
	Hub        *run.Hub
	Cancel     context.CancelFunc
	Inspect    bool
	OnStart    StartFn
	Catalog    provider.Status
	ListModels func(providerID string) ([]string, error)
}

type Model struct {
	phase    phase
	overlay  overlay
	input    textinput.Model
	spin     spinner.Model
	vp       viewport.Model
	goal     string
	provider string
	model    string
	hub      *run.Hub
	cancel   context.CancelFunc
	inspect  bool
	width    int
	height   int
	start    time.Time
	ready    bool
	err      error
	onStart  StartFn
	catalog  provider.Status
	listFn   func(string) ([]string, error)
	items    []pickItem
	cursor   int
	hint     string
	prefix   bool
	models   []string
}

func New(opts Options) Model {
	ti := textinput.New()
	ti.Placeholder = "what should temper do?"
	ti.CharLimit = 500
	ti.Width = 60
	ti.Focus()

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinStyle

	m := Model{
		phase:    phaseSplash,
		input:    ti,
		spin:     s,
		goal:     opts.Goal,
		provider: opts.Provider,
		model:    opts.Model,
		hub:      opts.Hub,
		cancel:   opts.Cancel,
		inspect:  opts.Inspect,
		width:    80,
		height:   24,
		start:    time.Now(),
		onStart:  opts.OnStart,
		catalog:  opts.Catalog,
		listFn:   opts.ListModels,
	}
	if opts.Inspect {
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

func (m Model) fetchModels() tea.Cmd {
	id := m.provider
	fn := m.listFn
	return func() tea.Msg {
		if fn == nil {
			return modelsMsg{}
		}
		names, err := fn(id)
		return modelsMsg{Models: names, Err: err}
	}
}

func (m Model) advanceSetup() (Model, tea.Cmd) {
	if m.provider == "" {
		if c, ok := m.catalog.BestCandidate(); ok && c.Usable {
			m.provider = c.ID
		} else {
			m.phase = phasePickProvider
			m.items = providerItems(m.catalog)
			m.cursor = firstUsable(m.items)
			m.hint = ""
			if len(m.catalog.Usable()) == 0 {
				m.hint = "no usable provider — pick a service or set a key"
			}
			return m, nil
		}
	}
	if m.model == "" {
		m.phase = phasePickModel
		m.items = nil
		m.cursor = 0
		m.hint = "loading models…"
		return m, m.fetchModels()
	}
	if m.goal == "" {
		m.phase = phaseInput
		m.input.Placeholder = "what should temper do?"
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink
	}
	return m.beginRun()
}

func (m Model) beginRun() (Model, tea.Cmd) {
	m.phase = phaseRunning
	if m.onStart != nil {
		m.onStart(m.goal, m.provider, m.model)
	}
	return m, nil
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
		return m.advanceSetup()

	case modelsMsg:
		if msg.Err != nil {
			m.hint = msg.Err.Error()
		} else {
			m.hint = ""
		}
		m.models = msg.Models
		if len(msg.Models) == 0 {
			m.phase = phaseModelInput
			m.input.Placeholder = "model id"
			m.input.SetValue("")
			m.input.Focus()
			if m.hint == "" {
				m.hint = "no models listed — type a model id"
			}
			return m, textinput.Blink
		}
		m.phase = phasePickModel
		m.items = modelItems(msg.Models)
		m.cursor = 0
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
		next, cmd := m.handleKey(msg)
		return next, cmd
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		if m.cancel != nil {
			m.cancel()
		}
		if m.phase == phaseRunning {
			return m, nil
		}
		return m, tea.Quit
	}

	if m.overlay == overlayQuit {
		switch key {
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
	if m.overlay != overlayNone && m.overlay != overlayQuit && !m.prefix {
		switch key {
		case "esc":
			m.overlay = overlayNone
			return m, nil
		}
	}

	if m.prefix {
		m.prefix = false
		cmd, ok := prefixCommand(key)
		if !ok {
			m.hint = "unknown prefix command"
			return m, nil
		}
		return m.runPrefix(cmd)
	}
	if isPrefixKey(key) {
		m.prefix = true
		return m, nil
	}

	if pickerPhase(m.phase) {
		return m.handlePicker(key)
	}

	if typingPhase(m.phase) {
		if key == "enter" {
			v := strings.TrimSpace(m.input.Value())
			if v == "" {
				return m, nil
			}
			if m.phase == phaseModelInput {
				m.model = v
				if m.goal == "" {
					m.phase = phaseInput
					m.input.Placeholder = "what should temper do?"
					m.input.SetValue("")
					return m, textinput.Blink
				}
				return m.beginRun()
			}
			m.goal = v
			return m.beginRun()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) runPrefix(cmd string) (tea.Model, tea.Cmd) {
	switch cmd {
	case "cancel":
		return m, nil
	case "help":
		if m.overlay == overlayHelp {
			m.overlay = overlayNone
		} else {
			m.overlay = overlayHelp
		}
		return m, nil
	case "status":
		if m.overlay == overlayStatus {
			m.overlay = overlayNone
		} else {
			m.overlay = overlayStatus
		}
		return m, nil
	case "eval":
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
	case "quit":
		if m.phase == phaseRunning && !m.inspect {
			m.overlay = overlayQuit
			return m, nil
		}
		return m, tea.Quit
	case "down":
		m.vp.LineDown(1)
		return m, nil
	case "up":
		m.vp.LineUp(1)
		return m, nil
	case "top":
		m.vp.GotoTop()
		return m, nil
	case "bottom":
		m.vp.GotoBottom()
		return m, nil
	}
	return m, nil
}

func (m Model) handlePicker(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		if m.cursor < len(m.items)-1 {
			m.cursor++
		}
		return m, nil
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "enter":
		if m.cursor < 0 || m.cursor >= len(m.items) {
			return m, nil
		}
		it := m.items[m.cursor]
		if m.phase == phasePickProvider {
			if !it.Usable {
				m.hint = it.Detail
				return m, nil
			}
			m.provider = it.ID
			m.model = ""
			return m.advanceSetup()
		}
		m.model = it.ID
		if m.goal == "" {
			m.phase = phaseInput
			m.input.Placeholder = "what should temper do?"
			m.input.SetValue("")
			m.input.Focus()
			return m, textinput.Blink
		}
		return m.beginRun()
	}
	return m, nil
}

func (m Model) View() string {
	if m.phase == phaseSplash {
		return m.viewSplash()
	}
	if pickerPhase(m.phase) {
		return m.viewPicker()
	}
	if typingPhase(m.phase) {
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
	b.WriteString(dimStyle.Render("  probing providers…"))
	if len(m.catalog.Candidates) > 0 {
		b.WriteString("\n\n")
		for _, c := range m.catalog.Candidates {
			mark := "·"
			if c.Usable {
				mark = "✓"
			}
			b.WriteString(dimStyle.Render(fmt.Sprintf("  %s  %-14s  %s\n", mark, c.ID, c.Reason)))
		}
	}
	return b.String()
}

func (m Model) viewPicker() string {
	title := "choose provider"
	if m.phase == phasePickModel {
		title = "choose model  ·  " + nz(m.provider, "provider")
	}
	var b strings.Builder
	b.WriteString(headerStyle.Render("TEMPER"))
	b.WriteString(dimStyle.Render("  ·  " + title))
	b.WriteString("\n\n")
	if len(m.items) == 0 {
		b.WriteString(dimStyle.Render("  loading…"))
	}
	for i, it := range m.items {
		cur := "  "
		if i == m.cursor {
			cur = "> "
		}
		line := fmt.Sprintf("%s%-16s  %s", cur, it.Title, it.Detail)
		if i == m.cursor {
			b.WriteString(bold.Render(line))
		} else if !it.Usable {
			b.WriteString(dimStyle.Render(line))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	if m.hint != "" {
		b.WriteString(yellow.Render(m.hint))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("j/k move  enter select  " + m.prefixHint()))
	return b.String()
}

func (m Model) viewInput() string {
	title := "new run"
	if m.phase == phaseModelInput {
		title = "model  ·  " + nz(m.provider, "provider")
	} else if m.provider != "" {
		title = nz(m.provider, "provider") + "  ·  " + nz(m.model, "model")
	}
	var b strings.Builder
	b.WriteString(headerStyle.Render("TEMPER"))
	b.WriteString(dimStyle.Render("  ·  " + title))
	b.WriteString("\n\n")
	b.WriteString(boxStyle.Render(m.input.View()))
	b.WriteString("\n\n")
	if m.hint != "" {
		b.WriteString(yellow.Render(m.hint))
		b.WriteString("\n")
	}
	b.WriteString(dimStyle.Render("enter to continue  ·  " + m.prefixHint()))
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
		short(snap.RunID, 14), snap.State, nz(snap.Agent, "native"), nz(nz(snap.Provider, m.provider), "—"), nz(nz(snap.Model, m.model), "—"))
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
	return fmt.Sprintf("%s %s  %d%%  %s  reflex %s  tok %d/%d  ·  %s",
		spin, bar, pct, elapsed, ref, snap.TokensWorker, snap.TokensJudge, m.prefixHint())
}

func (m Model) prefixHint() string {
	if m.prefix {
		return prefixStyle.Render("prefix") + dimStyle.Render("  s ? e q j k g G")
	}
	return "ctrl+b prefix  ·  ctrl+c cancel"
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
		snap.RunID, snap.State, snap.Agent, nz(snap.Provider, m.provider), nz(snap.Model, m.model), snap.Workspace,
		snap.BudgetUsed, snap.BudgetMax, snap.TokensWorker, snap.TokensJudge,
		snap.Reflex.State, strings.Join(snap.Reflex.Reasons, ", "),
		nz(snap.RecoveryRung, "—"), judge)
	return overlayBox("status", body, m.width)
}

func helpOverlay(width int) string {
	return overlayBox("keys", `ctrl+b  command prefix
then:   s status  ? help  e last eval
        j/k scroll  g/G top/bottom
        q quit  esc cancel prefix
ctrl+c  cancel run (no prefix)
enter   select / submit`, width)
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

func providerItems(st provider.Status) []pickItem {
	out := make([]pickItem, 0, len(st.Candidates))
	for _, c := range st.Candidates {
		detail := c.Reason
		if !c.Usable && c.Hint != "" {
			detail = c.Reason + " — " + c.Hint
		}
		out = append(out, pickItem{ID: c.ID, Title: c.ID, Detail: detail, Usable: c.Usable})
	}
	return out
}

func modelItems(names []string) []pickItem {
	out := make([]pickItem, 0, len(names))
	for _, n := range names {
		out = append(out, pickItem{ID: n, Title: n, Detail: "", Usable: true})
	}
	return out
}

func firstUsable(items []pickItem) int {
	for i, it := range items {
		if it.Usable {
			return i
		}
	}
	return 0
}

func Run(opts Options) error {
	p := tea.NewProgram(New(opts), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
