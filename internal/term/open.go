package term

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

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
func Open(argv []string, dir string) error {
	line := CommandLine(argv, dir)
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("open a terminal and run: %s", line)
	}
	app := ResolveApp()
	script := appleScript(app, line)
	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("open %s: %w: %s; run: %s", app, err, strings.TrimSpace(string(out)), line)
	}
	return nil
}

func appleScript(app, line string) string {
	esc := strings.ReplaceAll(line, `\`, `\\`)
	esc = strings.ReplaceAll(esc, `"`, `\"`)
	switch app {
	case "iTerm":
		return fmt.Sprintf(`tell application "iTerm"
activate
create window with default profile
tell current session of current window
write text "%s"
end tell
end tell`, esc)
	default:
		return fmt.Sprintf(`tell application "Terminal"
activate
do script "%s"
end tell`, esc)
	}
}
