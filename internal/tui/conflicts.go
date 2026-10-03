package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/git"
	"github.com/osesantos/kb/internal/tui/components"
)

// conflicts is the merge popup: unmerged files, whether each still holds markers, and a preview from the first marker.
type conflicts struct {
	top   string
	files []conflictFile
	sel   int
}

type conflictFile struct {
	path, text string
	resolved   bool
}

type pulledMsg struct {
	out string
	err error
}

// pull starts a background pull unless a pull or push is already running.
func (m *Model) pull() tea.Cmd {
	if m.pushing {
		return nil
	}
	if git.Merging(m.v.Root) {
		m.openConflicts()
		return nil
	}
	m.pushing = true
	m.info("pulling…")
	root := m.v.Root
	return func() tea.Msg {
		out, err := git.Pull(root)
		return pulledMsg{out, err}
	}
}

// autoPull pulls when the vault is a git repository with an upstream; otherwise it stays silent.
func (m *Model) autoPull() tea.Cmd {
	if !git.HasUpstream(m.v.Root) {
		return nil
	}
	return m.pull()
}

func (m *Model) pulled(msg pulledMsg) tea.Cmd {
	m.pushing = false
	switch {
	case msg.err != nil:
		m.fail("pull failed: %v", msg.err)
	case git.Merging(m.v.Root):
		m.openConflicts()
	default:
		m.info("pulled")
	}
	if g, ok := m.view.(*gitView); ok {
		g.refresh(m)
	}
	return tea.Batch(m.reload(), m.refreshDirty(), m.recount())
}

// openConflicts shows the popup, or reports that no merge is in progress.
func (m *Model) openConflicts() {
	top, ok := git.Toplevel(m.v.Root)
	if !ok || !git.Merging(m.v.Root) {
		m.info("no merge in progress")
		return
	}
	m.conflicts = &conflicts{top: top}
	m.conflicts.refresh(m)
}

// refresh re-reads the unmerged files and their markers, keeping the selection in range.
func (c *conflicts) refresh(m *Model) {
	paths, err := git.Conflicts(m.v.Root)
	if err != nil {
		m.fail("git conflicts: %v", err)
	}
	c.files = make([]conflictFile, len(paths))
	for i, p := range paths {
		b, _ := os.ReadFile(filepath.Join(c.top, p))
		c.files[i] = conflictFile{path: p, text: string(b), resolved: !git.HasMarkers(string(b))}
	}
	c.sel = min(c.sel, max(len(c.files)-1, 0))
}

func (c *conflicts) resolved() bool {
	return !slices.ContainsFunc(c.files, func(f conflictFile) bool { return !f.resolved })
}

// mergeContinue commits the merge once no file holds markers, then pushes.
func (m *Model) mergeContinue() tea.Cmd {
	if !git.Merging(m.v.Root) {
		m.info("no merge in progress")
		return nil
	}
	c := m.conflicts
	if c == nil {
		m.openConflicts()
		c = m.conflicts
	}
	c.refresh(m)
	if !c.resolved() {
		m.fail("conflict markers left in %d file(s)", len(slices.DeleteFunc(slices.Clone(c.files), func(f conflictFile) bool { return f.resolved })))
		return nil
	}
	paths := make([]string, len(c.files))
	for i, f := range c.files {
		paths[i] = f.path
	}
	out, err := git.FinishMerge(m.v.Root, paths)
	if err != nil {
		m.fail("merge commit failed: %v", err)
		return nil
	}
	m.conflicts = nil
	m.info("merged %s", strings.TrimSpace(out))
	if g, ok := m.view.(*gitView); ok {
		g.refresh(m)
	}
	return tea.Batch(m.refreshDirty(), m.push(m.v.Root))
}

func (m *Model) conflictsKey(k string) tea.Cmd {
	c := m.conflicts
	switch k {
	case "esc", "q":
		m.conflicts = nil
	case "down", "j", "ctrl+n":
		c.sel = min(c.sel+1, max(len(c.files)-1, 0))
	case "up", "k", "ctrl+p":
		c.sel = max(c.sel-1, 0)
	case "enter", "e":
		if c.sel < len(c.files) {
			return m.editCmd(filepath.Join(c.top, c.files[c.sel].path))
		}
	case "C":
		return m.mergeContinue()
	}
	return nil
}

func (m *Model) conflictsView() string {
	w := max(m.w*8/10, 50)
	return components.Modal(m.st, m.conflictsContent(w, max(m.h*3/4, 10)), w, 0)
}

// conflictsContent mirrors the search popup: title, files left, the selected file from its first marker right.
func (m *Model) conflictsContent(w, h int) string {
	c := m.conflicts
	bg := m.st.Modal.Box.GetBackground()
	listW := w * 2 / 5
	prevW := w - listW - 3
	bodyH := h - 3

	left := 0
	rows := make([]row, len(c.files))
	for i, f := range c.files {
		mark := m.st.Status.Error.Render("✗ ")
		if f.resolved {
			mark = m.st.Git.Staged.Render("✓ ")
		} else {
			left++
		}
		rows[i] = row{text: "  " + f.path, styled: mark + f.path}
	}
	title := m.st.Prompt.Label.Render("merge conflicts ") + m.st.Badge.Label.Render(strconv.Itoa(left)+" left")
	list := m.st.Empty.Hint.Render("no unmerged files")
	if len(rows) > 0 {
		list = m.listPane(listW, bodyH, rows, c.sel)
	}
	preview := ""
	if c.sel < len(c.files) {
		lines := strings.Split(c.files[c.sel].text, "\n")
		start := max(slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "<<<<<<< ") })-2, 0)
		styled := make([]string, 0, bodyH)
		for _, l := range lines[start:min(start+bodyH, len(lines))] {
			if git.HasMarkers(l) {
				l = m.st.Status.Error.Render(l)
			}
			styled = append(styled, l)
		}
		preview = strings.Join(styled, "\n")
	}
	fit := func(s string, width int) []string {
		lines := strings.Split(onBackground(s, bg, width), "\n")
		blank := onBackground("", bg, width)
		for len(lines) < bodyH {
			lines = append(lines, blank)
		}
		return lines[:bodyH]
	}
	l, r := fit(list, listW), fit(preview, prevW)
	gap, sep := onBackground(" ", bg, 1), m.st.Divider.Horizontal.Background(bg).Render("│")
	out := []string{onBackground(title, bg, w), onBackground(components.HorizontalDivider(m.st, w), bg, w)}
	for i := range bodyH {
		out = append(out, l[i]+gap+sep+gap+r[i])
	}
	help := "↑↓ move · ⏎ edit · esc close"
	if left == 0 {
		help = "↑↓ move · ⏎ edit · C merge continue + push · esc close"
	}
	return strings.Join(append(out, onBackground(m.st.Badge.Label.Render(help), bg, w)), "\n")
}
