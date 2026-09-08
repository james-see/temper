package debuglog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

func Requested(flag bool) bool {
	if flag {
		return true
	}
	v := strings.ToLower(strings.TrimSpace(os.Getenv("TEMPER_DEBUG")))
	return v == "1" || v == "true" || v == "yes"
}

// Setup writes verbose slog to stderr (and .temper/debug.log when dataDir is set).
// When tui is true, skip stderr so the alt screen stays readable.
func Setup(dataDir string, tui bool) (*slog.Logger, error) {
	var writers []io.Writer
	if !tui {
		writers = append(writers, os.Stderr)
	}
	if dataDir != "" {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(filepath.Join(dataDir, "debug.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		writers = append(writers, f)
	}
	if len(writers) == 0 {
		writers = append(writers, os.Stderr)
	}
	h := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: slog.LevelDebug})
	log := slog.New(h)
	slog.SetDefault(log)
	return log, nil
}
