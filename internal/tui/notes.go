package tui

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/tui/components"
	"github.com/osesantos/kb/internal/vault"
)

// notesView shows the vault's notes as a folder tree (filterable by path) and previews the selected note.
type notesView struct {
	filter string
	tree   components.TreeModel[treeItem]
	// saved is the expansion before a filter auto-expanded the tree; Esc restores it.
	saved map[string]bool
	// boardFocus points at the model's board focus so help can tell which left panel is active.
	boardFocus *bool
}

func newNotes(m *Model) *notesView {
	st := m.st
	n := &notesView{tree: components.NewTree(func(it treeItem, _, depth, width int, kids, open, focused bool) string {
		text := components.TreePrefix(depth, kids, open) + it.name
		switch {
		case focused:
			return st.ListRow.Selected.Width(width).Render(truncate(text, width))
		case it.note < 0:
			return st.Group.Header.Render(truncate(text, width))
		}
		return st.ListRow.Normal.Render(truncate(text, width))
	})}
	n.boardFocus = &m.board.focus
	n.refilter(m, "")
	return n
}

// matching lists the notes whose path contains every filter word.
func (n *notesView) matching(m *Model) []int {
	words := strings.Fields(strings.ToLower(n.filter))
	out := []int{}
	for i, note := range m.v.Notes {
		p := strings.ToLower(note.Path)
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(p, w) }) {
			out = append(out, i)
		}
	}
	return out
}

// refilter rebuilds the tree from the filter and reveals keep (a note path) when it is still shown.
// A filter expands everything so matches are visible; clearing it restores the earlier expansion.
func (n *notesView) refilter(m *Model, keep string) {
	n.tree = n.tree.SetNodes(buildNoteTree(m.v.Notes, n.matching(m)))
	switch {
	case n.filter != "" && n.saved == nil:
		n.saved = n.tree.Expansion()
		n.tree = n.tree.ExpandAll()
	case n.filter != "":
		n.tree = n.tree.ExpandAll()
	case n.saved != nil:
		n.tree = n.tree.WithExpansion(n.saved)
		n.saved = nil
	}
	if keep != "" {
		n.tree = n.tree.Reveal(keep)
	}
}

func (n *notesView) current() (int, bool) {
	it, ok := n.tree.Selected()
	if !ok || it.note < 0 {
		return 0, false
	}
	return it.note, true
}

func (n *notesView) currentPath(m *Model) string {
	if i, ok := n.current(); ok && i < len(m.v.Notes) {
		return m.v.Notes[i].Path
	}
	return ""
}

// selectNote moves the selection to a note, expanding its folders and clearing the filter when it hides that note.
func (n *notesView) selectNote(m *Model, idx int) {
	path := m.v.Notes[idx].Path
	if n.tree = n.tree.Reveal(path); n.tree.SelectedID() == path {
		return
	}
	n.filter = ""
	n.refilter(m, path)
}

func (n *notesView) beforeReload(m *Model) func(*Model) {
	cur := n.tree.SelectedID()
	note := n.currentPath(m)
	return func(m *Model) {
		n.tree = n.tree.SetNodes(buildNoteTree(m.v.Notes, n.matching(m)))
		if _, ok := m.v.Find(note); note != "" && !ok {
			if m.right {
				m.right = false
				m.fail("%s was removed", note)
			}
			return
		}
		n.tree = n.tree.Reveal(cur)
	}
}

func (n *notesView) crumb(m *Model) string {
	if p := n.currentPath(m); m.right && p != "" {
		return p
	}
	return "notes"
}

func (n *notesView) listTitle() string { return "Notes" }

func (n *notesView) list(_ *Model, w, h int) string {
	if n.tree.RowCount() == 0 {
		return ""
	}
	n.tree = n.tree.SetSize(w, h)
	return n.tree.View()
}

func (n *notesView) preview(m *Model) (string, string, func(int) string) {
	if m.board.focus {
		return m.boardPreview()
	}
	if i, ok := n.current(); ok {
		note := &m.v.Notes[i]
		return note.Title, note.Path, func(w int) string { return m.renderNote(i, w) }
	}
	it, ok := n.tree.Selected()
	if !ok {
		return "Preview", "none", func(int) string { return m.st.Empty.Hint.Render("no notes match") }
	}
	id := n.tree.SelectedID()
	return it.name, "dir|" + id, func(int) string {
		count := 0
		for _, note := range m.v.Notes {
			if strings.HasPrefix(note.Path, id+"/") {
				count++
			}
		}
		return m.st.Group.Header.Render(id+"/") + "\n\n" + m.st.Empty.Hint.Render(strconv.Itoa(count)+" notes · l expand · h collapse")
	}
}

var wikiRe = regexp.MustCompile(`(!?)\[\[([^\]|]*)(?:\|([^\]]*))?\]\]`)

// wikilinksToText turns `[[target|alias]]` into bold text and `![[file]]` into an italic marker, outside code fences.
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
	note := &m.v.Notes[i]
	out := m.markdown(width).Render(wikilinksToText(vault.StripFrontmatter(note.Body)))
	if bl := m.v.Backlinks[i]; len(bl) > 0 {
		out += "\n\n" + m.st.Group.Header.Render(m.st.Glyphs.Backlink+" Backlinks ("+strconv.Itoa(len(bl))+")")
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
	if m.board.focus {
		if cmd, used := m.boardKey(k); used {
			m.scroll = 0
			return cmd
		}
	}
	switch k {
	case "q":
		return tea.Quit
	case "tab":
		if boardFits(m.h - 2) {
			m.board.focus, m.scroll = true, 0
		}
	case "j", "down":
		n.tree, _ = n.tree.MoveCursor(1)
		m.scroll = 0
	case "k", "up":
		n.tree, _ = n.tree.MoveCursor(-1)
		m.scroll = 0
	case "g":
		n.tree, m.scroll = n.tree.SelectIndex(0), 0
	case "G":
		n.tree, m.scroll = n.tree.SelectIndex(n.tree.RowCount()-1), 0
	case "s":
		m.prompt = searchPrompt()
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
	case "l", "right", "enter":
		id, _, kids, open, ok := n.tree.Current()
		switch {
		case !ok:
		case kids && (k == "enter" || !open):
			n.tree = n.tree.SetExpanded(id, !open)
		case kids:
			n.tree, _ = n.tree.MoveCursor(1)
		case hasCur:
			m.open(cur)
		}
	case "h", "left":
		id, _, kids, open, ok := n.tree.Current()
		switch {
		case !ok:
		case kids && open:
			n.tree = n.tree.SetExpanded(id, false)
		case n.tree.ParentID(id) != "":
			n.tree = n.tree.SelectID(n.tree.ParentID(id))
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
	if k == "esc" && m.find.query != "" {
		m.find = find{}
		return nil
	}
	if k == "esc" || k == "q" || k == "h" || k == "left" {
		m.right = false
		if m.returnTo != nil {
			m.view, m.returnTo, m.find = m.returnTo, nil, find{}
		}
		return nil
	}
	if m.scrollKey(k) {
		return nil
	}
	switch k {
	case "/":
		m.prompt = &prompt{label: "find in note: ", done: func(m *Model, q string) tea.Cmd { m.runFind(q); return nil }}
	case "n":
		m.stepFind(1)
	case "N":
		m.stepFind(-1)
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
	if !right && n.boardFocus != nil && *n.boardFocus {
		return []hint{{"j/k", "day"}, {"h/l", "week"}, {"H/L", "month"}, {"t", "today"}, {"⏎", "daily note"}, {"e", "edit/create"}, {"tab", "tree"}}
	}
	if right {
		return []hint{{"j/k", "scroll"}, {"/", "find"}, {"tab", "links"}, {"[ ]", "history"}, {"e", "edit"}, {"esc", "back"}}
	}
	return []hint{{"j/k", "move"}, {"l/h", "open/close"}, {"tab", "board"}, {"/", "filter"}, {"s", "search"}, {"e", "edit"}, {":", "git cal vaults…"}, {"q", "quit"}}
}
