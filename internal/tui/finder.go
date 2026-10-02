package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

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
	w := max(m.w*8/10, 50)
	return components.Modal(m.st, m.finderContent(w, max(m.h*3/4, 10)), w, 0)
}

// finderContent is the popup's inside, every cell on the modal background.
func (m *Model) finderContent(w, h int) string {
	f := m.finder
	bg := m.st.Modal.Box.GetBackground()
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
		preview = m.st.Group.Header.Render(n.Path) + "\n" + m.markdown(prevW).Render(wikilinksToText(vault.StripFrontmatter(n.Sections[h.Section].Text(n))))
	}
	fit := func(s string, width int) []string {
		lines := strings.Split(onBackground(s, bg, width), "\n")
		blank := onBackground("", bg, width)
		for len(lines) < bodyH {
			lines = append(lines, blank)
		}
		return lines[:bodyH]
	}
	left, right := fit(list, listW), fit(preview, prevW)
	gap, sep := onBackground(" ", bg, 1), m.st.Divider.Horizontal.Background(bg).Render("│")
	out := []string{onBackground(input, bg, w), onBackground(components.HorizontalDivider(m.st, w), bg, w)}
	for i := range bodyH {
		out = append(out, left[i]+gap+sep+gap+right[i])
	}
	return strings.Join(append(out, onBackground(m.st.Badge.Label.Render("↑↓ move · ⏎ open · tab all results · esc close"), bg, w)), "\n")
}
