package tui

import (
	"strings"

	"github.com/osesantos/kb/internal/tui/styles"

	"github.com/charmbracelet/x/ansi"
)

// scrollKey handles the preview pane's scrolling keys; it reports whether k was one of them.
func (m *Model) scrollKey(k string) bool {
	page := max(m.rh, 1)
	switch k {
	case "j", "down":
		m.scroll++
	case "k", "up":
		m.scroll = max(m.scroll-1, 0)
	case "space", "pgdown":
		m.scroll += page
	case "pgup":
		m.scroll = max(m.scroll-page, 0)
	case "g":
		m.scroll = 0
	case "G":
		m.scroll = 1 << 30
	default:
		return false
	}
	return true
}

// previewKey handles a key while the preview pane is focused: back to the list, or scroll.
func (m *Model) previewKey(k string) {
	switch k {
	case "esc", "q", "h", "left":
		m.right = false
	default:
		m.scrollKey(k)
	}
}

// markdown is the glamour renderer for width, rebuilt only when the width changes.
func (m *Model) markdown(width int) styles.Markdown {
	if m.md.Width() != width {
		m.md = m.st.NewMarkdown(width)
	}
	return m.md
}

// moveSel moves a list selection for the standard list keys; it reports whether k was one of them.
func (m *Model) moveSel(sel *int, n int, k string) bool {
	switch k {
	case "j", "down":
		*sel = min(*sel+1, n-1)
	case "k", "up":
		*sel = max(*sel-1, 0)
	case "g":
		*sel = 0
	case "G":
		*sel = max(n-1, 0)
	default:
		return false
	}
	m.scroll = 0
	return true
}

// runFind records the preview lines containing q and jumps to the first; glamour output cannot be highlighted inline.
func (m *Model) runFind(q string) {
	m.find = find{query: q}
	if strings.TrimSpace(q) == "" {
		return
	}
	_, lines := m.previewLines(m.rw)
	needle := strings.ToLower(q)
	for i, l := range lines {
		if strings.Contains(strings.ToLower(ansi.Strip(l)), needle) {
			m.find.hits = append(m.find.hits, i)
		}
	}
	m.jumpFind()
}

func (m *Model) stepFind(d int) {
	if n := len(m.find.hits); n > 0 {
		m.find.cur = (m.find.cur + d%n + n) % n
		m.jumpFind()
	}
}

func (m *Model) jumpFind() {
	if m.find.cur < len(m.find.hits) {
		m.scroll = max(m.find.hits[m.find.cur]-m.rh/3, 0)
	}
}

// diffText colours a unified diff with the theme's git styles.
func (m *Model) diffText(d string) string {
	g := m.st.Git
	lines := strings.Split(d, "\n")
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---") || strings.HasPrefix(l, "diff ") || strings.HasPrefix(l, "index "):
			lines[i] = g.Meta.Render(l)
		case strings.HasPrefix(l, "+"):
			lines[i] = g.Added.Render(l)
		case strings.HasPrefix(l, "-"):
			lines[i] = g.Removed.Render(l)
		case strings.HasPrefix(l, "@"):
			lines[i] = g.Hunk.Render(l)
		}
	}
	return strings.Join(lines, "\n")
}

// backToNotes is the Esc/q behaviour of every non-notes view's list pane.
func (m *Model) backToNotes() { m.goTo(newNotes(m)) }
