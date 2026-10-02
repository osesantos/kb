package tui

import (
	"fmt"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/search"
	"github.com/osesantos/kb/internal/vault"
)

// searchView lists BM25-ranked sections; the preview shows the selected section.
type searchView struct {
	query  string
	hits   []search.Hit
	selIdx int
}

func (m *Model) search(q string) {
	m.goTo(&searchView{query: q, hits: search.Lexical(m.v, q, 500)})
}

func (s *searchView) crumb(*Model) string { return fmt.Sprintf("search %q (%d)", s.query, len(s.hits)) }
func (s *searchView) listTitle() string   { return "Search" }
func (s *searchView) sel() int            { return s.selIdx }

func (s *searchView) rows(m *Model) []row {
	rows := make([]row, len(s.hits))
	for i, h := range s.hits {
		n := &m.v.Notes[h.Note]
		text := n.Title
		if head := n.Sections[h.Section].Heading; head != "" && head != n.Title {
			text += " › " + head
		}
		aux := n.Path
		if h.Line > 0 {
			aux = fmt.Sprintf("%s:%d", n.Path, h.Line)
		}
		rows[i] = row{text: text, aux: aux}
	}
	return rows
}

func (s *searchView) hit() (search.Hit, bool) {
	if s.selIdx < 0 || s.selIdx >= len(s.hits) {
		return search.Hit{}, false
	}
	return s.hits[s.selIdx], true
}

func (s *searchView) preview(m *Model) (string, string, func(int) string) {
	h, ok := s.hit()
	if !ok {
		return "Preview", "none", func(int) string { return m.st.Empty.Hint.Render("no matches") }
	}
	n := &m.v.Notes[h.Note]
	sec := n.Sections[h.Section]
	return n.Title, fmt.Sprintf("%s|%d", n.Path, h.Section), func(w int) string {
		return m.markdown(w).Render(wikilinksToText(vault.StripFrontmatter(sec.Text(n))))
	}
}

func (s *searchView) beforeReload(m *Model) func(*Model) {
	var path string
	if h, ok := s.hit(); ok {
		path = m.v.Notes[h.Note].Path
	}
	return func(m *Model) {
		s.hits = search.Lexical(m.v, s.query, 500)
		s.selIdx = 0
		for i, h := range s.hits {
			if m.v.Notes[h.Note].Path == path {
				s.selIdx = i
				break
			}
		}
	}
}

func (s *searchView) key(m *Model, k string) tea.Cmd {
	h, ok := s.hit()
	if m.right {
		m.previewKey(k)
		return nil
	}
	if m.moveSel(&s.selIdx, len(s.hits), k) {
		return nil
	}
	switch k {
	case "esc", "q":
		m.backToNotes()
	case "s":
		m.prompt = searchPrompt()
	case "l", "right":
		m.right = true
	case "enter":
		if ok {
			m.returnTo = s
			m.open(h.Note)
			m.runFind(s.query)
		}
	case "e":
		if ok {
			return m.editCmd(filepath.Join(m.v.Root, m.v.Notes[h.Note].Path))
		}
	}
	return nil
}

func (s *searchView) help(right bool) []hint {
	if right {
		return []hint{{"j/k", "scroll"}, {"esc", "back"}}
	}
	return []hint{{"j/k", "move"}, {"⏎", "open note"}, {"→", "preview"}, {"s", "new search"}, {"e", "edit"}, {"esc", "notes"}}
}
