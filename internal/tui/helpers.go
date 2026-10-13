package tui

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/osesantos/kb/internal/tui/styles"
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
	if m.plain {
		return styles.Markdown{}
	}
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

// runFind records every occurrence of q in the preview and jumps to the first.
func (m *Model) runFind(q string) {
	m.find = find{query: q}
	m.refind()
	m.jumpFind()
}

// refind recomputes the hits for the current query, keeping the current hit's line as close as possible.
func (m *Model) refind() {
	line := 0
	if m.find.cur < len(m.find.hits) {
		line = m.find.hits[m.find.cur].line
	}
	m.find.hits, m.find.cur = nil, 0
	if strings.TrimSpace(m.find.query) == "" {
		return
	}
	_, lines := m.previewLines(m.rw)
	m.find.hits = findHits(lines, m.find.query)
	m.find.cur = max(slices.IndexFunc(m.find.hits, func(h hit) bool { return h.line >= line }), 0)
}

// findHits returns every case-insensitive occurrence of q in lines, as cell ranges of the visible text.
func findHits(lines []string, q string) []hit {
	needle := strings.ToLower(q)
	var hits []hit
	for i, l := range lines {
		text := strings.ToLower(ansi.Strip(l))
		for off := 0; ; {
			j := strings.Index(text[off:], needle)
			if j < 0 {
				break
			}
			start := ansi.StringWidth(text[:off+j])
			hits = append(hits, hit{i, start, start + ansi.StringWidth(needle)})
			off += j + len(needle)
		}
	}
	return hits
}

// highlight paints the find hits that fall on lines (which start at preview line first).
func (m *Model) highlight(lines []string, first int) []string {
	if len(m.find.hits) == 0 {
		return lines
	}
	out := slices.Clone(lines)
	ranges := map[int][]lipgloss.Range{}
	for j, h := range m.find.hits {
		if h.line < first || h.line >= first+len(lines) {
			continue
		}
		st := m.st.Find.Match
		if j == m.find.cur {
			st = m.st.Find.Current
		}
		ranges[h.line-first] = append(ranges[h.line-first], lipgloss.NewRange(h.start, h.end, st))
	}
	for i, r := range ranges {
		out[i] = lipgloss.StyleRanges(out[i], r...)
	}
	return out
}

func (m *Model) stepFind(d int) {
	if n := len(m.find.hits); n > 0 {
		m.find.cur = (m.find.cur + d%n + n) % n
		m.jumpFind()
	}
}

func (m *Model) jumpFind() {
	if m.find.cur < len(m.find.hits) {
		m.scroll = max(m.find.hits[m.find.cur].line-m.rh/3, 0)
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
