package term

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type Handle struct {
	App string
	TTY string
}

func (h *Handle) CanType() bool {
	return h != nil && strings.TrimSpace(h.TTY) != ""
}

func ResolveApp() string {
	if v := strings.TrimSpace(os.Getenv("TEMPER_TERMINAL")); v != "" {
		return normalizeApp(v)
	}
	return normalizeApp(os.Getenv("TERM_PROGRAM"))
}

func normalizeApp(s string) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "iterm.app", "iterm":
		return "iTerm"
	case "apple_terminal", "terminal.app", "terminal":
		return "Terminal"
	case "ghostty":
		return "Ghostty"
	default:
		if s == "" {
			return "Terminal"
		}
		return s
	}
}

func Quote(argv []string) string {
	var b strings.Builder
	for i, a := range argv {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(quote(a))
	}
	return b.String()
}

func quote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func CommandLine(argv []string, dir string) string {
	cmd := Quote(argv)
	if dir == "" {
		return cmd
	}
	return "cd " + quote(dir) + " && " + cmd
}

// Open launches argv in a new terminal window. Fail-soft: caller should print CommandLine on error.
func Open(argv []string, dir string) (*Handle, error) {
	line := CommandLine(argv, dir)
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("open a terminal and run: %s", line)
	}
	app := ResolveApp()
	script := appleScript(app, line)
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w: %s; run: %s", app, err, strings.TrimSpace(string(out)), line)
	}
	return &Handle{App: app, TTY: parseTTY(string(out))}, nil
}

func appleScript(app, line string) string {
	esc := appleEscape(line)
	switch app {
	case "iTerm":
		return fmt.Sprintf(`tell application "iTerm"
activate
create window with default profile
tell current session of current window
write text "%s"
return tty
end tell
end tell`, esc)
	default:
		return fmt.Sprintf(`tell application "Terminal"
activate
set newTab to do script "%s"
return tty of newTab
end tell`, esc)
	}
}

func (h *Handle) Type(text string) error {
	if !h.CanType() {
		return fmt.Errorf("no terminal handle")
	}
	if h.App == "iTerm" {
		script := typeITermScript(h.TTY, text)
		cmd := exec.Command("osascript", "-e", script)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("type iTerm: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return typeTTY(h.TTY, text)
}

func typeITermScript(tty, text string) string {
	return fmt.Sprintf(`tell application "iTerm"
repeat with w in windows
repeat with t in tabs of w
repeat with s in sessions of t
if tty of s is "%s" then
tell s
write text "%s"
end tell
return
end if
end repeat
end repeat
end repeat
error "hermes terminal session not found"
end tell`, appleEscape(tty), appleEscape(text))
}

func typeTTY(tty, text string) error {
	f, err := os.OpenFile(tty, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("type %s: %w", tty, err)
	}
	defer f.Close()
	if _, err := f.WriteString(strings.TrimRight(text, "\r\n") + "\r"); err != nil {
		return fmt.Errorf("type %s: %w", tty, err)
	}
	return nil
}

func appleEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", ` `)
	s = strings.ReplaceAll(s, "\r", ` `)
	return s
}

func parseTTY(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/dev/tty") {
			return line
		}
	}
	return ""
}
