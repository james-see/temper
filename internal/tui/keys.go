package tui

const prefixKey = "ctrl+b"

func isPrefixKey(s string) bool { return s == prefixKey }

func prefixCommand(s string) (string, bool) {
	switch s {
	case "s":
		return "status", true
	case "?":
		return "help", true
	case "e":
		return "eval", true
	case "q":
		return "quit", true
	case "j", "down":
		return "down", true
	case "k", "up":
		return "up", true
	case "g":
		return "top", true
	case "G":
		return "bottom", true
	case "esc":
		return "cancel", true
	default:
		return "", false
	}
}

func typingPhase(p phase) bool {
	return p == phaseInput || p == phaseModelInput
}

func pickerPhase(p phase) bool {
	return p == phasePickProvider || p == phasePickModel
}
