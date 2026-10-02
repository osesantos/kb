package tui

import (
	"path/filepath"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/vault"
)

// notesView lists the vault's notes (filterable by path) and previews the selected one.
type notesView struct {
	filter  string
	visible []int
	selIdx  int
}

func newNotes(m *Model) *notesView {
	n := &notesView{}
	n.refilter(m, "")
	return n
}

// refilter recomputes the visible notes from the filter and keeps the selection on keep (a note path) when still shown.
func (n *notesView) refilter(m *Model, keep string) {
	words := strings.Fields(strings.ToLower(n.filter))
	n.visible = n.visible[:0]
	for i, note := range m.v.Notes {
		p := strings.ToLower(note.Path)
		ok := true
		for _, w := range words {
			ok = ok && strings.Contains(p, w)
		}
		if ok {
			n.visible = append(n.visible, i)
		}
	}
	n.selIdx = 0
	for si, i := range n.visible {
		if m.v.Notes[i].Path == keep {
			n.selIdx = si
		}
	}
}

func (n *notesView) current() (int, bool) {
	if n.selIdx < 0 || n.selIdx >= len(n.visible) {
		return 0, false
	}
	return n.visible[n.selIdx], true
}

func (n *notesView) currentPath(m *Model) string {
	if i, ok := n.current(); ok && i < len(m.v.Notes) {
		return m.v.Notes[i].Path
	}
	return ""
}

// selectNote moves the selection to a note, clearing the filter when it hides that note.
func (n *notesView) selectNote(m *Model, idx int) {
	for si, i := range n.visible {
		if i == idx {
			n.selIdx = si
			return
		}
	}
	n.filter = ""
	n.refilter(m, m.v.Notes[idx].Path)
}

func (n *notesView) crumb(m *Model) string {
	if p := n.currentPath(m); m.right && p != "" {
		return p
	}
	return "notes"
}

func (n *notesView) listTitle() string { return "Notes" }

func (n *notesView) rows(m *Model) []row {
	rows := make([]row, len(n.visible))
	for si, i := range n.visible {
		note := m.v.Notes[i]
		dir := filepath.Dir(note.Path)
		if dir == "." {
			dir = ""
		}
		rows[si] = row{text: note.Title, aux: dir}
	}
	return rows
}

func (n *notesView) sel() int { return n.selIdx }

func (n *notesView) move(m *Model, d int) {
	n.selIdx = max(min(n.selIdx+d, len(n.visible)-1), 0)
	m.scroll = 0
}

func (n *notesView) preview(m *Model) (string, string, func(int) string) {
	i, ok := n.current()
	if !ok {
		return "Preview", "none", func(int) string { return "" }
	}
	note := &m.v.Notes[i]
	return note.Title, note.Path, func(w int) string { return m.renderNote(i, w) }
}

var wikiRe = regexp.MustCompile(`(!?)\[\[([^\]|]*)(?:\|([^\]]*))?\]\]`)

// wikilinksToText turns `[[target|alias]]` into bold text, and `![[file]]` into an italic marker, outside code fences,
// because a Markdown renderer prints wikilinks raw.
// ponytail: inline code containing [[x]] is rewritten too; fences are the common case.
func wikilinksToText(src string) string {
	fence := false
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
			continue
		}
		if fence {
			continue
		}
		lines[i] = wikiRe.ReplaceAllStringFunc(l, func(s string) string {
			g := wikiRe.FindStringSubmatch(s)
			label := g[2]
			if g[3] != "" {
				label = g[3]
			}
			if g[1] == "!" {
				return "*⧉ " + label + "*"
			}
			return "**" + label + "**"
		})
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderNote(i, width int) string {
	if m.md.Width() != width {
		m.md = m.st.NewMarkdown(width)
	}
	note := &m.v.Notes[i]
	out := m.md.Render(wikilinksToText(vault.StripFrontmatter(note.Body)))
	if bl := m.v.Backlinks[i]; len(bl) > 0 {
		out += "\n\n" + m.st.Group.Header.Render(m.st.Glyphs.Backlink+" Backlinks ("+itoa(len(bl))+")")
		for _, b := range bl {
			out += "\n  " + m.v.Notes[b].Title
		}
	}
	return out
}

func (n *notesView) key(m *Model, k string) tea.Cmd {
	cur, hasCur := n.current()
	if m.right {
		return n.rightKey(m, k, cur, hasCur)
	}
	switch k {
	case "q":
		return tea.Quit
	case "j", "down":
		n.move(m, 1)
	case "k", "up":
		n.move(m, -1)
	case "g":
		n.selIdx, m.scroll = 0, 0
	case "G":
		n.selIdx, m.scroll = max(len(n.visible)-1, 0), 0
	case "/":
		m.prompt = &prompt{
			label: "/", text: n.filter,
			done: func(*Model, string) tea.Cmd { return nil },
			live: func(m *Model, t string) { n.filter = t; n.refilter(m, n.currentPath(m)) },
		}
	case "esc":
		if n.filter != "" {
			n.filter = ""
			n.refilter(m, n.currentPath(m))
		}
	case "enter", "l", "right":
		if hasCur {
			m.open(cur)
		}
	case "e":
		if hasCur {
			return m.editCmd(filepath.Join(m.v.Root, m.v.Notes[cur].Path))
		}
	case "[":
		m.back(-1)
	case "]":
		m.back(1)
	}
	return nil
}

func (n *notesView) rightKey(m *Model, k string, cur int, hasCur bool) tea.Cmd {
	page := max(m.h-6, 1)
	switch k {
	case "esc", "q", "h", "left":
		m.right = false
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
	case "tab", "enter":
		if hasCur {
			m.picker = newPicker(m, cur)
		}
	case "e":
		if hasCur {
			return m.editCmd(filepath.Join(m.v.Root, m.v.Notes[cur].Path))
		}
	case "[":
		m.back(-1)
	case "]":
		m.back(1)
	}
	return nil
}

func (n *notesView) help(right bool) []hint {
	if right {
		return []hint{{"j/k", "scroll"}, {"tab", "links"}, {"[ ]", "history"}, {"e", "edit"}, {"esc", "back"}, {":", "command"}}
	}
	return []hint{{"j/k", "move"}, {"/", "filter"}, {"⏎", "open"}, {"e", "edit"}, {"[ ]", "history"}, {":", "command"}, {"q", "quit"}}
}
