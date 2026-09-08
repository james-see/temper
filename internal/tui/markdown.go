package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

type mdCache struct {
	mu sync.Mutex
	w  int
	r  *glamour.TermRenderer
}

var markdown mdCache

func paneInnerWidth(vp viewport.Model) int {
	w := vp.Width - vp.Style.GetHorizontalFrameSize()
	if w < 8 {
		return 8
	}
	return w
}

func wrapLines(s string, width int) []string {
	return wrapWords(s, width)
}

func wrapWords(s string, width int) []string {
	if width < 8 {
		width = 8
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		para = strings.TrimRight(para, "\r")
		if strings.TrimSpace(para) == "" {
			out = append(out, "")
			continue
		}
		var line strings.Builder
		lineW := 0
		flush := func() {
			if line.Len() == 0 {
				return
			}
			out = append(out, line.String())
			line.Reset()
			lineW = 0
		}
		for _, word := range strings.Fields(para) {
			ww := lipgloss.Width(word)
			if lineW == 0 {
				if ww <= width {
					line.WriteString(word)
					lineW = ww
					continue
				}
				chunks := hardWrap(word, width)
				for i, c := range chunks {
					if i < len(chunks)-1 {
						out = append(out, c)
						continue
					}
					line.WriteString(c)
					lineW = lipgloss.Width(c)
				}
				continue
			}
			if lineW+1+ww <= width {
				line.WriteByte(' ')
				line.WriteString(word)
				lineW += 1 + ww
				continue
			}
			flush()
			if ww <= width {
				line.WriteString(word)
				lineW = ww
				continue
			}
			chunks := hardWrap(word, width)
			for i, c := range chunks {
				if i < len(chunks)-1 {
					out = append(out, c)
					continue
				}
				line.WriteString(c)
				lineW = lipgloss.Width(c)
			}
		}
		flush()
	}
	return out
}

func hardWrap(s string, width int) []string {
	var out []string
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > width && w > 0 {
			out = append(out, b.String())
			b.Reset()
			w = 0
		}
		b.WriteRune(r)
		w += rw
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

func clipWidth(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	return runewidth.Truncate(s, width, "…")
}

func renderMarkdown(src string, width int) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return ""
	}
	if width < 8 {
		width = 8
	}
	r, err := markdown.renderer(width)
	if err != nil {
		return strings.Join(wrapWords(src, width), "\n")
	}
	out, err := r.Render(src)
	if err != nil {
		return strings.Join(wrapWords(src, width), "\n")
	}
	return trimRightSpaceLines(strings.TrimSpace(out))
}

func trimRightSpaceLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

func (c *mdCache) renderer(width int) (*glamour.TermRenderer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.r != nil && c.w == width {
		return c.r, nil
	}
	style := styles.DarkStyleConfig
	zero := uint(0)
	style.Document.Margin = &zero
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	c.r, c.w = r, width
	return r, nil
}
