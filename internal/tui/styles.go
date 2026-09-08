package tui

import "github.com/charmbracelet/lipgloss"

var (
	wordmark = `
████████╗███████╗███╗   ███╗██████╗ ███████╗██████╗
╚══██╔══╝██╔════╝████╗ ████║██╔══██╗██╔════╝██╔══██╗
   ██║   █████╗  ██╔████╔██║██████╔╝█████╗  ██████╔╝
   ██║   ██╔══╝  ██║╚██╔╝██║██╔═══╝ ██╔══╝  ██╔══██╗
   ██║   ███████╗██║ ╚═╝ ██║██║     ███████╗██║  ██║
   ╚═╝   ╚══════╝╚═╝     ╚═╝╚═╝     ╚══════╝╚═╝  ╚═╝
`

	headerStyle   = lipgloss.NewStyle().Bold(true)
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	spinStyle     = lipgloss.NewStyle()
	wordmarkStyle = lipgloss.NewStyle().Bold(true)
	prefixStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#1B7F3A", Dark: "#4ADE80"})
	boxStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	vpStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

	green  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1B7F3A", Dark: "#4ADE80"})
	yellow = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A16207", Dark: "#FBBF24"})
	red    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"})
	bold   = lipgloss.NewStyle().Bold(true)
)

func reflexStyle(state string) lipgloss.Style {
	switch state {
	case "progressing", "complete":
		return green
	case "uncertain", "stalled":
		return yellow
	case "looping", "regressing":
		return red
	default:
		return dimStyle
	}
}

func eventStyle(typ string) lipgloss.Style {
	switch typ {
	case "loop.detected", "regression.detected", "run.failed":
		return red
	case "progress.evaluated", "stagnation.detected":
		return yellow
	case "run.completed", "recovery.completed":
		return green
	case "tool.requested", "tool.completed", "model.called", "user.message":
		return bold
	default:
		return lipgloss.NewStyle()
	}
}
