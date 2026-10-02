package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/osesantos/kb/internal/search"
	"github.com/osesantos/kb/internal/tui/components"
	"github.com/osesantos/kb/internal/vault"
)

const finderLimit = 100

// finder is the Telescope-style search popup: a query line, live-ranked sections, and a preview of the selected one.
type finder struct {
	query string
	hits  []search.Hit
	sel   int
}

func (m *Model) openFinder() { m.finder = &finder{} }

func (f *finder) rerun(m *Model) {
	f.hits = search.Lexical(m.v, f.query, finderLimit)
	f.sel = 0
}

func (m *Model) finderKey(k string) tea.Cmd {
	f := m.finder
	switch k {
	case "esc":
		m.finder = nil
	case "down", "ctrl+n", "ctrl+j":
		f.sel = min(f.sel+1, len(f.hits)-1)
	case "up", "ctrl+p", "ctrl+k":
		f.sel = max(f.sel-1, 0)
	case "enter":
		if f.sel < len(f.hits) {
			m.finder = nil
			m.open(f.hits[f.sel].Note)
			m.runFind(f.query)
		}
	case "tab":
		if f.query != "" {
			m.finder = nil
			m.search(f.query)
		}
	case "backspace":
		if r := []rune(f.query); len(r) > 0 {
			f.query = string(r[:len(r)-1])
			f.rerun(m)
		}
	case "ctrl+u":
		f.query = ""
		f.rerun(m)
	case "space":
		f.query += " "
		f.rerun(m)
	default:
		if r := []rune(k); len(r) == 1 {
			f.query += k
			f.rerun(m)
		}
	}
	return nil
}

func (m *Model) finderPaste(s string) {
	m.finder.query += strings.NewReplacer("\r", "", "\n", " ").Replace(s)
	m.finder.rerun(m)
}

// finderView lays the popup out at about 80% × 75% of the screen: query on top, results left, preview right.
func (m *Model) finderView() string {
	f := m.finder
	w := max(m.w*8/10, 50)
	h := max(m.h*3/4, 10)
	listW := w * 2 / 5
	prevW := w - listW - 3
	bodyH := h - 3

	count := ""
	if f.query != "" {
		count = fmt.Sprintf("  %d", len(f.hits))
	}
	input := m.st.Prompt.Label.Render("› ") + m.st.Prompt.Text.Render(f.query+"█") + m.st.Badge.Label.Render(count)
	rows := make([]row, len(f.hits))
	for i, h := range f.hits {
		n := &m.v.Notes[h.Note]
		text := n.Title
		if head := n.Sections[h.Section].Heading; head != "" && head != n.Title {
			text += " › " + head
		}
		rows[i] = row{text: text, aux: n.Path}
	}
	var list string
	switch {
	case f.query == "":
		list = m.st.Empty.Hint.Render("type to search")
	case len(rows) == 0:
		list = m.st.Empty.Hint.Render("no matches")
	default:
		list = m.listPane(listW, bodyH, rows, f.sel)
	}
	preview := ""
	if f.sel < len(f.hits) {
		h := f.hits[f.sel]
		n := &m.v.Notes[h.Note]
		lines := strings.Split(m.markdown(prevW).Render(wikilinksToText(vault.StripFrontmatter(n.Sections[h.Section].Text(n)))), "\n")
		preview = m.st.Group.Header.Render(truncate(n.Path, prevW)) + "\n" + truncateLines(strings.Join(lines[:min(len(lines), bodyH-1)], "\n"), prevW)
	}
	left := lipgloss.NewStyle().Width(listW).Height(bodyH).MaxHeight(bodyH).Render(list)
	right := lipgloss.NewStyle().Width(prevW).Height(bodyH).MaxHeight(bodyH).Render(preview)
	sep := m.st.Divider.Horizontal.Render(strings.Repeat("│\n", bodyH-1) + "│")
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", sep, " ", right)
	help := m.st.Badge.Label.Render("↑↓ move · ⏎ open · tab all results · esc close")
	return components.Modal(m.st, strings.Join([]string{input, components.HorizontalDivider(m.st, w), body, help}, "\n"), w, 0)
}
