package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/james-see/temper/internal/agent"
	"github.com/james-see/temper/internal/event"
	"github.com/james-see/temper/internal/provider"
	"github.com/james-see/temper/internal/run"
)

type phase int

const (
	phaseSplash phase = iota
	phasePickProvider
	phasePickModel
	phasePickSession
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
	overlayRecover
)

type pane int

const (
	paneLog pane = iota
	paneIO
	paneSessions
)

const attachAllID = "*"

type tickMsg time.Time

type catalogMsg struct{}

type modelsMsg struct {
	Models []string
	Err    error
}

type cursorSessionsMsg struct {
	Picks []agent.CursorPick
	Err   error
}

type pickItem struct {
	ID     string
	Title  string
	Detail string
	Meta   string
	Usable bool
}

type StartFn func(goal, provider, model, cursorWS, cursorSess string)
type FollowFn func(text string)
type ModeFn func() string
type ApproveFn func()

type Options struct {
	Goal       string
	Agent      string
	Provider   string
	Model      string
	Hub        *run.Hub
	Cancel     context.CancelFunc
	Inspect    bool
	OnStart    StartFn
	OnFollow   FollowFn
	OnMode     ModeFn
	OnApprove  ApproveFn
	Catalog    provider.Status
	ListModels func(providerID string) ([]string, error)
	ListCursor func() ([]agent.CursorPick, error)
	CursorSess string
	CursorWS   string
}

type Model struct {
	phase      phase
	overlay    overlay
	input      textinput.Model
	spin       spinner.Model
	vp         viewport.Model
	goal       string
	agent      string
	provider   string
	model      string
	hub        *run.Hub
	cancel     context.CancelFunc
	inspect    bool
	width      int
	height     int
	start      time.Time
	ready      bool
	err        error
	onStart    StartFn
	onFollow   FollowFn
	onMode     ModeFn
	onApprove  ApproveFn
	external   bool
	catalog    provider.Status
	listFn     func(string) ([]string, error)
	listCursor func() ([]agent.CursorPick, error)
	cursorSess string
	cursorWS   string
	items      []pickItem
	cursor     int
	hint       string
	prefix     bool
	models     []string
	pane       pane
	follow     bool
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
		phase:      phaseSplash,
		input:      ti,
		spin:       s,
		goal:       opts.Goal,
		agent:      opts.Agent,
		provider:   opts.Provider,
		model:      opts.Model,
		external:   agent.IsExternal(opts.Agent),
		onMode:     opts.OnMode,
		onApprove:  opts.OnApprove,
		hub:        opts.Hub,
		cancel:     opts.Cancel,
		inspect:    opts.Inspect,
		width:      80,
		height:     24,
		start:      time.Now(),
		onStart:    opts.OnStart,
		onFollow:   opts.OnFollow,
		catalog:    opts.Catalog,
		listFn:     opts.ListModels,
		listCursor: opts.ListCursor,
		cursorSess: opts.CursorSess,
		cursorWS:   opts.CursorWS,
		follow:     true,
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

func (m Model) fetchCursorSessions() tea.Cmd {
	fn := m.listCursor
	return func() tea.Msg {
		if fn == nil {
			return cursorSessionsMsg{Err: fmt.Errorf("Cursor is not open or no sessions found")}
		}
		picks, err := fn()
		return cursorSessionsMsg{Picks: picks, Err: err}
	}
}

func (m Model) advanceSetup() (Model, tea.Cmd) {
	if agent.TypeOf(m.agent) == "cursor" {
		if m.cursorSess != "" {
			return m.beginRun()
		}
		m.phase = phasePickSession
		m.hint = "scanning open Cursor windows…"
		return m, m.fetchCursorSessions()
	}
	if m.external {
		return m.beginRun()
	}
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
		m.onStart(m.goal, m.provider, m.model, m.cursorWS, m.cursorSess)
	}
	return m, nil
}

func (m Model) toGoalInput() (Model, tea.Cmd) {
	m.phase = phaseInput
	m.goal = ""
	m.overlay = overlayNone
	m.prefix = false
	m.hint = ""
	m.input.Placeholder = "what should temper do?"
	m.input.SetValue("")
	m.input.Focus()
	return m, textinput.Blink
}

func (m *Model) layout() {
	headerH, footerH := 3, 4
	replyH := 0
	if m.canReply() {
		replyH = 3
	}
	h := max(3, m.height-headerH-footerH-replyH)
	if !m.ready {
		m.vp = viewport.New(max(20, m.width-2), h)
		m.vp.Style = vpStyle
		m.ready = true
	} else {
		m.vp.Width = max(20, m.width-2)
		m.vp.Height = h
	}
	m.input.Width = max(20, m.width-8)
	if m.follow {
		m.vp.GotoBottom()
	}
}

func (m Model) canReply() bool {
	return m.phase == phaseDone && !m.inspect && !m.external
}

func (m *Model) armReply() {
	m.pane = paneIO
	m.input.Placeholder = "follow up on this thread…"
	m.input.SetValue("")
	m.input.Focus()
	m.syncPane()
}

func (m Model) pendingRecovery() bool {
	if m.hub == nil {
		return false
	}
	return m.hub.Get().PendingRecovery != ""
}

func (m Model) approvePending() (tea.Model, tea.Cmd) {
	if m.onApprove != nil {
		m.onApprove()
	}
	m.overlay = overlayNone
	m.hint = ""
	return m, nil
}

func (m Model) beginFollow(text string) (Model, tea.Cmd) {
	m.input.SetValue("")
	m.input.Blur()
	m.phase = phaseRunning
	m.pane = paneIO
	m.layout()
	if m.onFollow != nil {
		m.onFollow(text)
	}
	return m, nil
}

func (m Model) togglePane() (Model, tea.Cmd) {
	switch m.pane {
	case paneLog:
		m.pane = paneIO
	case paneIO:
		m.pane = paneSessions
	default:
		m.pane = paneLog
	}
	m.syncPane()
	return m, nil
}

func (m *Model) syncPane() {
	if m.hub == nil || !m.ready {
		return
	}
	m.refreshPane(m.hub.Get())
}

func (m *Model) refreshPane(snap Snapshot) {
	inner := paneInnerWidth(m.vp)
	switch m.pane {
	case paneIO:
		wait := ""
		if !snap.Done && m.phase == phaseRunning {
			wait = m.chatWaitLine()
		}
		m.vp.SetContent(renderModelIO(snap, inner, wait))
	case paneSessions:
		m.vp.SetContent(renderSessions(snap, inner))
	default:
		m.vp.SetContent(renderEvents(snap.Events, inner))
	}
	if m.follow {
		m.vp.GotoBottom()
	}
}

func (m Model) chatWaitLine() string {
	sparks := []string{"✦", "✧", "✶", "⋆", "✦", "·"}
	i := int(time.Since(m.start)/(140*time.Millisecond)) % len(sparks)
	if i < 0 {
		i = 0
	}
	return m.spin.View() + "  " + sparks[i] + "  composing"
}

func (m *Model) pinFollow() {
	m.follow = m.vp.AtBottom()
}

func (m *Model) handleScroll(key string) bool {
	letters := m.phase == phaseRunning || m.inspect
	switch key {
	case "up":
		m.vp.LineUp(1)
	case "down":
		m.vp.LineDown(1)
	case "k":
		if !letters {
			return false
		}
		m.vp.LineUp(1)
	case "j":
		if !letters {
			return false
		}
		m.vp.LineDown(1)
	case "pgup", "ctrl+u":
		m.vp.HalfPageUp()
	case "pgdown", "pgdn", "ctrl+d":
		m.vp.HalfPageDown()
	case "home":
		m.vp.GotoTop()
		m.follow = false
		return true
	case "end":
		m.vp.GotoBottom()
		m.follow = true
		return true
	case "g":
		if !letters {
			return false
		}
		m.vp.GotoTop()
		m.follow = false
		return true
	case "G":
		if !letters {
			return false
		}
		m.vp.GotoBottom()
		m.follow = true
		return true
	default:
		return false
	}
	m.pinFollow()
	return true
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
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

	case cursorSessionsMsg:
		return m.applyCursorSessions(msg)

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tickMsg:
		if m.hub != nil && m.ready && (m.phase == phaseRunning || m.phase == phaseDone || m.inspect) {
			snap := m.hub.Get()
			m.refreshPane(snap)
			if snap.PendingRecovery != "" && m.overlay == overlayNone && m.phase == phaseRunning {
				m.overlay = overlayRecover
			}
			if snap.PendingRecovery == "" && m.overlay == overlayRecover {
				m.overlay = overlayNone
			}
			if snap.Done && m.phase == phaseRunning {
				m.phase = phaseDone
				if !m.external {
					m.armReply()
				}
				m.layout()
				return m, tea.Batch(tickEvery(), textinput.Blink)
			}
		}
		return m, tickEvery()

	case tea.MouseMsg:
		if m.phase == phaseRunning || m.phase == phaseDone || m.inspect {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			m.pinFollow()
			return m, cmd
		}

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
	if m.overlay == overlayRecover && !m.prefix {
		switch key {
		case "enter", "y", "Y":
			return m.approvePending()
		case "esc":
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

	if m.phase == phaseRunning && (key == "enter" || key == "y") && m.pendingRecovery() {
		return m.approvePending()
	}

	if (m.phase == phaseRunning || m.phase == phaseDone) && key == "tab" {
		return m.togglePane()
	}

	if (m.phase == phaseRunning || m.phase == phaseDone || m.inspect) && m.handleScroll(key) {
		return m, nil
	}

	if m.phase == phaseDone && !m.inspect {
		if key == "enter" {
			v := strings.TrimSpace(m.input.Value())
			if v == "" {
				return m, nil
			}
			return m.beginFollow(v)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
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
	case "pane":
		return m.togglePane()
	case "log":
		m.pane = paneLog
		m.syncPane()
		return m, nil
	case "io":
		m.pane = paneIO
		m.syncPane()
		return m, nil
	case "sessions":
		m.pane = paneSessions
		m.syncPane()
		return m, nil
	case "mode":
		if m.onMode != nil {
			mode := m.onMode()
			m.hint = "reflex " + mode
		}
		return m, nil
	case "approve":
		return m.approvePending()
	case "new":
		if m.phase == phaseDone && !m.inspect {
			return m.toGoalInput()
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
		m.pinFollow()
		return m, nil
	case "up":
		m.vp.LineUp(1)
		m.pinFollow()
		return m, nil
	case "top":
		m.vp.GotoTop()
		m.follow = false
		return m, nil
	case "bottom":
		m.vp.GotoBottom()
		m.follow = true
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
		return m.selectPicker()
	}
	if n := digitIndex(key); n >= 0 && n < len(m.items) {
		m.cursor = n
		return m.selectPicker()
	}
	return m, nil
}

func (m Model) selectPicker() (tea.Model, tea.Cmd) {
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
	if m.phase == phasePickSession {
		if it.ID == attachAllID {
			m.cursorSess = "*"
			m.cursorWS = ""
		} else {
			m.cursorSess = it.ID
			m.cursorWS = it.Meta
		}
		return m.beginRun()
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

func (m Model) applyCursorSessions(msg cursorSessionsMsg) (Model, tea.Cmd) {
	if msg.Err != nil {
		m.hint = msg.Err.Error()
		m.items = nil
		return m, nil
	}
	if len(msg.Picks) == 0 {
		m.hint = "no composer sessions in open Cursor windows"
		m.items = nil
		return m, nil
	}
	if len(msg.Picks) == 1 {
		m.cursorSess = msg.Picks[0].SessionID
		m.cursorWS = msg.Picks[0].Workspace
		return m.beginRun()
	}
	m.phase = phasePickSession
	m.items = cursorSessionItems(msg.Picks)
	m.cursor = 0
	m.hint = fmt.Sprintf("%d open sessions — attach one or all", len(msg.Picks))
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
	case overlayRecover:
		return stack(body, m.recoverOverlay())
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
	} else if m.phase == phasePickSession {
		title = "attach session  ·  cursor"
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
	b.WriteString(dimStyle.Render("j/k move  1-9 jump  enter select  " + m.prefixHint()))
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
	agentName := nz(snap.Agent, nz(m.agent, "native"))
	prov := "—"
	if !m.external {
		prov = nz(nz(snap.Provider, m.provider), "—")
	} else if n := len(snap.Sessions); n > 1 {
		prov = fmt.Sprintf("%d sessions", n)
	} else if snap.SessionID != "" {
		prov = short(snap.SessionID, 18)
	}
	goal := nz(snap.ActiveGoal, snap.Goal)
	if goal == "" {
		goal = "—"
	}
	header := fmt.Sprintf("TEMPER  ·  %s  ·  %s  ·  %s  ·  %s  ·  %s  ·  %s  ·  %s",
		short(snap.RunID, 14), snap.State, agentName, short(goal, 36), paneLabel(m.pane), scrollLabel(m), nz(prov, "—"))
	var b strings.Builder
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n")
	if m.ready {
		b.WriteString(m.vp.View())
	}
	if m.canReply() {
		b.WriteString("\n")
		b.WriteString(boxStyle.Render(m.input.View()))
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
	mode := nz(snap.ReflexMode, "human")
	hint := m.prefixHint()
	if snap.PendingRecovery != "" && !m.prefix {
		hint = "enter approve  ·  ctrl+b a  ·  " + hint
	} else if snap.Done && !m.inspect && !m.prefix && !m.external {
		hint = "enter reply  ·  ↑↓ scroll  ·  end latest  ·  ctrl+b n  ·  tab  ·  ctrl+c"
	} else if !m.prefix {
		hint = "↑↓ scroll  ·  G latest  ·  tab  ·  " + hint
	}
	if m.ready && !m.vp.AtBottom() {
		hint = "more ↓  ·  " + hint
	}
	return fmt.Sprintf("%s %s  %d%%  %s  reflex %s %s  in %s / out %s  ·  %s",
		spin, bar, pct, elapsed, mode, ref, compactTok(snap.TokensPrompt), compactTok(snap.TokensCompletion), hint)
}

func (m Model) prefixHint() string {
	if m.prefix {
		return prefixStyle.Render("prefix") + dimStyle.Render("  s ? e a y n t l w q j k g G")
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
goal       %s
active     %s
agent      %s
provider   %s
model      %s
workspace  %s
budget     %.4f / %.2f
tokens     in %d  out %d  judge %d
reflex     %s  %s
recovery   %s
pending    %s
mode       %s
session    %s
sessions   %d
attach     %s
judge      %s`,
		snap.RunID, snap.State, snap.Goal, nz(snap.ActiveGoal, snap.Goal), nz(snap.Agent, m.agent), nz(snap.Provider, m.provider), nz(snap.Model, m.model), snap.Workspace,
		snap.BudgetUsed, snap.BudgetMax, snap.TokensPrompt, snap.TokensCompletion, snap.TokensJudge,
		snap.Reflex.State, strings.Join(snap.Reflex.Reasons, ", "),
		nz(snap.RecoveryRung, "—"), nz(snap.PendingRecovery, "—"), nz(snap.ReflexMode, "human"),
		nz(snap.SessionID, "—"), len(snap.Sessions), nz(snap.Attach, "—"), judge)
	return overlayBox("status", body, m.width)
}

func helpOverlay(width int) string {
	return overlayBox("keys", `ctrl+b  command prefix
then:   s status  ? help  e last eval
        t model io  l event log  w sessions
        j/k scroll  g/G top/bottom
        a toggle reflex human/auto  y approve recovery
        n new goal  q quit  esc cancel prefix
↑↓      scroll pane  ·  pgup/pgdn page
end     jump to latest (re-enables follow)
g / G   top / latest while a run is live
wheel   scroll  ·  live follow until you scroll up
tab     cycle log / model io / sessions
ctrl+c  cancel run (no prefix)
enter   reply in this thread when done`, width)
}

func quitOverlay(width int) string {
	return overlayBox("quit", "cancel run and quit?  y / n", width)
}

func (m Model) recoverOverlay() string {
	snap := Snapshot{}
	if m.hub != nil {
		snap = snapFrom(m.hub.Get())
	}
	reasons := strings.Join(snap.PendingReasons, ", ")
	if reasons == "" {
		reasons = strings.Join(snap.Reflex.Reasons, ", ")
	}
	body := fmt.Sprintf("%s  ·  %s\n%s\n\nenter / ctrl+b y approve  ·  ctrl+b a auto  ·  esc hide",
		nz(string(snap.Reflex.State), "reflex"), nz(snap.PendingRecovery, "recover"), nz(reasons, "pending recovery"))
	return overlayBox("recover", body, m.width)
}

type Snapshot = run.Snapshot

func snapFrom(s run.Snapshot) Snapshot { return s }

func paneLabel(p pane) string {
	switch p {
	case paneIO:
		return "io"
	case paneSessions:
		return "sessions"
	default:
		return "log"
	}
}

func scrollLabel(m Model) string {
	if !m.ready {
		return "live"
	}
	if m.vp.AtBottom() {
		return "live"
	}
	return fmt.Sprintf("%d%%", int(m.vp.ScrollPercent()*100))
}

func renderSessions(snap Snapshot, width int) string {
	if width < 24 {
		width = 24
	}
	if len(snap.Sessions) == 0 {
		return dimStyle.Render("no sessions attached")
	}
	var b strings.Builder
	b.WriteString(bold.Render(fmt.Sprintf("%d connected  ·  tab cycles log / io / sessions", len(snap.Sessions))))
	b.WriteString("\n\n")
	for _, s := range snap.Sessions {
		mark := " "
		if s.Focus || (snap.FocusSession != "" && s.Session == snap.FocusSession) {
			mark = "*"
		}
		head := fmt.Sprintf("%s %-8s  %-16s  %s", mark, nz(s.Agent, "—"), nz(s.Label, "—"), short(s.Session, 36))
		if mark == "*" {
			b.WriteString(bold.Render(clipWidth(head, width)))
		} else {
			b.WriteString(clipWidth(head, width))
		}
		b.WriteString("\n")
		if s.Preview != "" {
			b.WriteString(dimStyle.Render("      " + clipWidth(s.Preview, width-6)))
			b.WriteString("\n")
		}
		meta := nz(s.Attach, "attached")
		if s.Workspace != "" {
			meta += "  ·  " + s.Workspace
		}
		b.WriteString(dimStyle.Render("      " + clipWidth(meta, width-6)))
		b.WriteString("\n")
	}
	return b.String()
}

func renderEvents(evs []event.Event, width int) string {
	if len(evs) == 0 {
		return dimStyle.Render("waiting for events…")
	}
	if width < 24 {
		width = 24
	}
	var b strings.Builder
	for _, ev := range evs {
		head := fmt.Sprintf("%-4d  %s", ev.Sequence, ev.Type)
		if extra := eventHeadline(ev); extra != "" {
			head += "  " + extra
		}
		b.WriteString(eventStyle(ev.Type).Render(clipWidth(head, width)))
		b.WriteString("\n")
		for _, line := range eventBodyLines(ev, 5, width-6) {
			b.WriteString(dimStyle.Render("      " + line))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderModelIO(snap Snapshot, width int, waiting string) string {
	if width < 24 {
		width = 24
	}
	var b strings.Builder
	goal := snap.Goal
	if goal == "" {
		goal = eventGoal(snap.Events)
	}
	if goal != "" {
		writeChatTurn(&b, "you", goal, width, true)
	}
	turns := 0
	for _, ev := range snap.Events {
		switch ev.Type {
		case event.UserMessage:
			var d struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			if strings.TrimSpace(d.Text) == "" {
				continue
			}
			turns++
			writeChatTurn(&b, "you", strings.TrimSpace(d.Text), width, true)
		case event.ModelCompleted:
			var d struct {
				Content   string `json:"content"`
				Error     string `json:"error"`
				ToolCalls int    `json:"tool_calls"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			if d.Error != "" {
				writeChatTurn(&b, "model", d.Error, width, false)
				continue
			}
			if strings.TrimSpace(d.Content) == "" && d.ToolCalls > 0 {
				continue
			}
			if strings.TrimSpace(d.Content) == "" {
				continue
			}
			turns++
			writeChatTurn(&b, "model", strings.TrimSpace(d.Content), width, true)
		case event.ToolRequested:
			var d struct {
				Tool string `json:"tool"`
				Args string `json:"args"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			cmd := toolArgPreview(d.Args)
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ·  %s  %s", d.Tool, cmd)))
			b.WriteString("\n")
		}
	}
	if waiting != "" {
		writeChatWait(&b, waiting, width)
	}
	if turns == 0 && goal == "" && waiting == "" {
		return dimStyle.Render("no model output yet")
	}
	return b.String()
}

func writeChatTurn(b *strings.Builder, role, body string, width int, markdown bool) {
	chip, bar := youChip, youBar
	if role == "model" {
		chip, bar = modelChip, modelBar
	}
	b.WriteString(chip.Render(role))
	b.WriteString("\n")
	body = strings.TrimSpace(body)
	if body == "" {
		b.WriteString("\n")
		return
	}
	writeChatBody(b, bar, body, width, markdown)
}

func writeChatWait(b *strings.Builder, waiting string, width int) {
	b.WriteString(modelChip.Render("model"))
	b.WriteString("\n")
	writeChatBody(b, modelBar, waiting, width, false)
}

func writeChatBody(b *strings.Builder, bar lipgloss.Style, body string, width int, markdown bool) {
	prefixW := 2
	inner := width - prefixW
	if inner < 8 {
		inner = 8
	}
	text := body
	if markdown {
		text = renderMarkdown(body, inner)
	} else {
		text = strings.Join(wrapWords(body, inner), "\n")
	}
	paint := func(line string) string {
		if !markdown && strings.Contains(body, "composing") {
			return waitStyle.Render(line)
		}
		return line
	}
	for _, line := range strings.Split(text, "\n") {
		b.WriteString(bar.Render("▍"))
		b.WriteString(" ")
		b.WriteString(paint(line))
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func eventHeadline(ev event.Event) string {
	var d map[string]any
	_ = json.Unmarshal(ev.Data, &d)
	switch ev.Type {
	case event.ToolRequested:
		return fmt.Sprintf("%v  %s", d["tool"], toolArgPreview(asString(d["args"])))
	case event.ToolCompleted:
		if err, ok := d["error"].(string); ok && err != "" {
			return "error"
		}
		return fmt.Sprintf("%v  %vms", d["tool"], d["ms"])
	case event.UserMessage:
		text := asString(d["text"])
		if k := asString(d["kind"]); k != "" && k != "ack" {
			return k + "  " + text
		}
		return text
	case event.GoalShifted:
		from, to := asString(d["from"]), asString(d["to"])
		if from == "" {
			return "new  " + to
		}
		return from + " → " + to
	case event.ModelThinking:
		return fmt.Sprintf("think  %v", d["chars"])
	case event.ModelCompleted:
		if err, ok := d["error"].(string); ok && err != "" {
			return "error"
		}
		return fmt.Sprintf("tools %v", d["tool_calls"])
	case event.ProgressEvaluated:
		return fmt.Sprintf("%v  %v", d["state"], d["reasons"])
	case event.RecoveryStarted, event.RecoveryCompleted, event.RecoveryFailed:
		action := asString(d["action"])
		if ev.Type == event.RecoveryFailed {
			if k := asString(d["kind"]); k != "" && k != "inject" {
				return action + "  " + k
			}
		}
		return action
	case event.AgentStarted:
		if e := asString(d["error"]); e != "" {
			return e
		}
		if s := asString(d["session"]); s != "" {
			return s
		}
	case event.RunFailed:
		return asString(d["error"])
	}
	return ""
}

func eventBodyLines(ev event.Event, maxLines, width int) []string {
	var d map[string]any
	_ = json.Unmarshal(ev.Data, &d)
	text := ""
	switch ev.Type {
	case event.UserMessage:
		text = asString(d["text"])
	case event.ToolCompleted:
		if err, ok := d["error"].(string); ok && err != "" {
			text = err
		} else {
			text = asString(d["output"])
		}
	case event.ModelCompleted:
		if err, ok := d["error"].(string); ok && err != "" {
			text = err
		} else {
			text = asString(d["content"])
		}
	case event.RunFailed, event.AgentStarted, event.RecoveryFailed:
		text = asString(d["error"])
		if text == "" {
			text = asString(d["command"])
		}
	default:
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	lines := wrapLines(text, width)
	if len(lines) > maxLines {
		kept := lines[:maxLines]
		kept[maxLines-1] = kept[maxLines-1] + fmt.Sprintf("  +%d", len(lines)-maxLines)
		return kept
	}
	return lines
}

func eventGoal(evs []event.Event) string {
	for _, ev := range evs {
		if ev.Type != event.RunCreated {
			continue
		}
		var d struct {
			Goal string `json:"goal"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		return d.Goal
	}
	return ""
}

func toolArgPreview(args string) string {
	var in struct {
		Command string `json:"command"`
		Path    string `json:"path"`
	}
	if json.Unmarshal([]byte(args), &in) == nil {
		if in.Command != "" {
			return in.Command
		}
		if in.Path != "" {
			return in.Path
		}
	}
	return strings.ReplaceAll(strings.TrimSpace(args), "\n", " ")
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
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

func compactTok(n int) string {
	if n >= 10000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
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

func cursorSessionItems(picks []agent.CursorPick) []pickItem {
	items := []pickItem{{
		ID:     attachAllID,
		Title:  "attach all",
		Detail: fmt.Sprintf("%d sessions across open windows", len(picks)),
		Usable: true,
	}}
	for _, p := range picks {
		items = append(items, pickItem{
			ID:     p.SessionID,
			Title:  nz(p.Label, "cursor"),
			Detail: ageSince(p.ModTime) + "  ·  " + nz(p.Preview, short(p.SessionID, 12)),
			Meta:   p.Workspace,
			Usable: true,
		})
	}
	return items
}

func digitIndex(key string) int {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return -1
	}
	return int(key[0] - '1')
}

func ageSince(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
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
	p := tea.NewProgram(New(opts), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
