package tui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/git"
)

// activityView lists the latest commits touching the vault, attributed by their Agent/Run trailers.
type activityView struct {
	commits []git.LogEntry
	selIdx  int
}

func (m *Model) openActivity() {
	a := &activityView{}
	m.goTo(a)
	a.load(m)
}

// load re-reads the commit log, keeping the selection on the same commit.
func (a *activityView) load(m *Model) {
	cs, err := git.Log(m.v.Root, 200)
	if err != nil {
		m.goTo(newNotes(m))
		m.fail("activity: %v", err)
		return
	}
	keep := ""
	if c, ok := a.commit(); ok {
		keep = c.Hash
	}
	a.commits = cs
	a.selIdx = max(slices.IndexFunc(cs, func(c git.LogEntry) bool { return c.Hash == keep }), 0)
}

func (a *activityView) commit() (git.LogEntry, bool) {
	if a.selIdx < 0 || a.selIdx >= len(a.commits) {
		return git.LogEntry{}, false
	}
	return a.commits[a.selIdx], true
}

func (a *activityView) onCommit(m *Model)   { a.load(m) }
func (a *activityView) crumb(*Model) string { return "activity" }
func (a *activityView) listTitle() string   { return "Activity" }
func (a *activityView) sel() int            { return a.selIdx }

func who(c git.LogEntry) string {
	switch {
	case c.Agent != "" && c.Run != "":
		return c.Agent + " · " + c.Run
	case c.Agent != "":
		return c.Agent
	}
	return "external"
}

func (a *activityView) rows(m *Model) []row {
	rows := make([]row, len(a.commits))
	for i, c := range a.commits {
		when := time.Unix(c.At, 0).Format("Jan 02 15:04")
		w := who(c)
		style := m.st.ListRow.Aux
		if c.Agent != "" {
			style = m.st.Group.Header
		}
		rows[i] = row{
			text:   when + "  " + w + "  " + c.Subject,
			styled: m.st.ListRow.Aux.Render(when+"  ") + style.Render(w) + "  " + c.Subject,
		}
	}
	return rows
}

func (a *activityView) preview(m *Model) (string, string, func(int) string) {
	c, ok := a.commit()
	if !ok {
		return "Commit", "none", func(int) string { return m.st.Empty.Hint.Render("no commits touch this vault yet") }
	}
	return c.Hash, c.Hash, func(int) string {
		d, err := git.Show(m.v.Root, c.Hash)
		if err != nil {
			return m.st.Status.Error.Render(err.Error())
		}
		return m.diffText(d)
	}
}

func (a *activityView) key(m *Model, k string) tea.Cmd {
	if m.right {
		m.previewKey(k)
		return nil
	}
	if m.moveSel(&a.selIdx, len(a.commits), k) {
		return nil
	}
	switch k {
	case "esc", "q":
		m.backToNotes()
	case "enter", "l", "right":
		m.right = true
	}
	return nil
}

func (a *activityView) help(right bool) []hint {
	if right {
		return []hint{{"j/k", "scroll"}, {"esc", "back"}}
	}
	return []hint{{"j/k", "move"}, {"⏎", "show commit"}, {"esc", "notes"}}
}
