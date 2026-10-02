package tui

import (
	"path/filepath"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/git"
)

// gitView lists the vault's working-tree changes; the preview shows the selected file's diff.
type gitView struct {
	top     string
	entries []git.Entry
	selIdx  int
}

func (m *Model) openGit() {
	top, ok := git.Toplevel(m.v.Root)
	if !ok {
		m.fail("not a git repository")
		return
	}
	g := &gitView{top: top}
	m.goTo(g)
	g.refresh(m)
}

// refresh re-reads git status, keeping the selection on the same path.
func (g *gitView) refresh(m *Model) {
	st, err := git.Status(m.v.Root)
	if err != nil {
		m.fail("git status: %v", err)
		return
	}
	keep := ""
	if e, ok := g.entry(); ok {
		keep = e.Path
	}
	g.entries = st
	g.selIdx = max(slices.IndexFunc(st, func(e git.Entry) bool { return e.Path == keep }), 0)
	m.gen++
}

func (g *gitView) entry() (git.Entry, bool) {
	if g.selIdx < 0 || g.selIdx >= len(g.entries) {
		return git.Entry{}, false
	}
	return g.entries[g.selIdx], true
}

func (g *gitView) beforeReload(*Model) func(*Model) { return func(m *Model) { g.refresh(m) } }
func (g *gitView) crumb(*Model) string              { return "git" }
func (g *gitView) listTitle() string                { return "Changes" }
func (g *gitView) sel() int                         { return g.selIdx }

func (g *gitView) rows(m *Model) []row {
	rows := make([]row, len(g.entries))
	for i, e := range g.entries {
		rows[i] = row{
			text:   string(e.X) + string(e.Y) + " " + e.Path,
			styled: m.st.Git.Staged.Render(string(e.X)) + m.st.Git.Unstaged.Render(string(e.Y)) + " " + e.Path,
		}
	}
	return rows
}

func (g *gitView) preview(m *Model) (string, string, func(int) string) {
	e, ok := g.entry()
	if !ok {
		return "Diff", "none", func(int) string { return m.st.Empty.Hint.Render("nothing to commit, working tree clean") }
	}
	key := string(e.X) + string(e.Y) + e.Path
	return e.Path, key, func(int) string {
		d, err := git.Diff(m.v.Root, e)
		if err != nil {
			return m.st.Status.Error.Render(err.Error())
		}
		return m.diffText(d)
	}
}

func (g *gitView) result(m *Model, what string, out string, err error) tea.Cmd {
	if err != nil {
		m.fail("%s failed: %v", what, err)
	} else if out != "" {
		m.info("%s: %s", what, out)
	} else {
		m.info("%s", what)
	}
	g.refresh(m)
	return m.refreshDirty()
}

func (g *gitView) key(m *Model, k string) tea.Cmd {
	if m.right {
		m.previewKey(k)
		return nil
	}
	if m.moveSel(&g.selIdx, len(g.entries), k) {
		return nil
	}
	e, ok := g.entry()
	root := m.v.Root
	switch k {
	case "esc", "q":
		m.backToNotes()
	case "enter", "l", "right":
		m.right = true
	case "space":
		if !ok {
			return nil
		}
		if e.Staged() {
			out, err := git.Unstage(root, e.Path)
			return g.result(m, "unstaged", out, err)
		}
		out, err := git.Stage(root, e.Path)
		return g.result(m, "staged", out, err)
	case "a":
		out, err := git.StageAll(root)
		return g.result(m, "staged all", out, err)
	case "c":
		m.prompt = &prompt{label: "commit message: ", done: func(m *Model, msg string) tea.Cmd {
			if msg == "" {
				m.fail("commit aborted: empty message")
				return nil
			}
			out, err := git.Commit(root, msg)
			return g.result(m, "committed", out, err)
		}}
	case "p":
		if m.pushing {
			return nil
		}
		m.pushing = true
		m.info("pushing…")
		return func() tea.Msg {
			out, err := git.Push(root)
			return pushedMsg{out, err}
		}
	case "e":
		if ok {
			return m.editCmd(filepath.Join(g.top, e.Path))
		}
	}
	return nil
}

func (g *gitView) help(right bool) []hint {
	if right {
		return []hint{{"j/k", "scroll"}, {"esc", "back"}}
	}
	return []hint{{"j/k", "move"}, {"⏎", "diff"}, {"space", "stage/unstage"}, {"a", "stage all"}, {"c", "commit"}, {"p", "push"}, {"e", "edit"}, {"esc", "notes"}}
}
