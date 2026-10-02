package tui

import (
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/tui/components"
	"github.com/osesantos/kb/internal/vault"
)

type pickItem struct {
	label  string
	prefix string
	target vault.Target
	raw    string
}

// picker is the modal list of a note's outgoing links and backlinks.
type picker struct {
	items []pickItem
	sel   int
}

func newPicker(m *Model, note int) *picker {
	g := m.st.Glyphs
	p := &picker{}
	for _, raw := range vault.RefLinks(m.v.Notes[note].Body) {
		t := m.v.Target(raw)
		it := pickItem{label: raw, target: t, raw: raw, prefix: g.Link}
		switch t.Kind {
		case vault.NoteTarget:
			it.label = m.v.Notes[t.Note].Title
		case vault.FileTarget:
			it.label, it.prefix = filepath.Base(t.Path), "⧉"
		case vault.URLTarget:
			it.prefix = "↗"
		case vault.Missing:
			it.prefix = g.Missing
		}
		p.items = append(p.items, it)
	}
	for _, b := range m.v.Backlinks[note] {
		p.items = append(p.items, pickItem{label: m.v.Notes[b].Title, prefix: g.Backlink, target: vault.Target{Kind: vault.NoteTarget, Note: b}})
	}
	return p
}

func (m *Model) pickerKey(k string) tea.Cmd {
	p := m.picker
	switch k {
	case "esc", "q", "tab":
		m.picker = nil
	case "j", "down":
		p.sel = min(p.sel+1, len(p.items)-1)
	case "k", "up":
		p.sel = max(p.sel-1, 0)
	case "enter":
		if len(p.items) == 0 {
			m.picker = nil
			return nil
		}
		it := p.items[p.sel]
		m.picker = nil
		switch it.target.Kind {
		case vault.NoteTarget:
			m.open(it.target.Note)
		case vault.FileTarget, vault.URLTarget:
			m.external(it.target.Path)
		default:
			m.fail("unresolved: %s", it.raw)
		}
	}
	return nil
}

func (m *Model) pickerView() string {
	p := m.picker
	if len(p.items) == 0 {
		return components.Modal(m.st, m.st.Empty.Hint.Render("no links in this note"), 30, 0)
	}
	h := min(len(p.items), max(m.h-10, 3))
	off := max(min(p.sel-h/2, len(p.items)-h), 0)
	width := 24
	for _, it := range p.items {
		width = max(width, lipgloss.Width(it.label)+4)
	}
	width = min(width, max(m.w-16, 24))
	lines := []string{m.st.Group.Header.Render("Links"), ""}
	for i := off; i < off+h; i++ {
		it := p.items[i]
		text := truncate(it.prefix+" "+it.label, width)
		if i == p.sel {
			lines = append(lines, m.st.ListRow.Selected.Width(width).Render(text))
		} else {
			lines = append(lines, m.st.ListRow.Normal.Render(text))
		}
	}
	lines = append(lines, "", m.st.Badge.Label.Render("⏎ follow · esc close"))
	return components.Modal(m.st, strings.Join(lines, "\n"), width, 0)
}
