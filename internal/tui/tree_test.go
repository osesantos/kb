package tui

import (
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/osesantos/kb/internal/config"
	"github.com/osesantos/kb/internal/vault"
)

func treeApp(t *testing.T) (string, *Model) {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	write(t, d, "Root note.md", "see [[Deep]]")
	write(t, d, "plans/Alpha.md", "# Alpha")
	write(t, d, "plans/sub/Deep.md", "# Deep\n\nbottom")
	write(t, d, "Zeta/z.md", "z")
	m := New(config.Config{}, vault.Load(d), nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return d, m
}

func TestTreeStartsCollapsedFoldersFirst(t *testing.T) {
	_, m := treeApp(t)
	s := screen(m)
	assert.Regexp(t, `(?s)▶ plans.*▶ Zeta.*Root note`, s)
	assert.NotContains(t, s, "Alpha")
}

func TestTreeExpandCollapseAndParentJump(t *testing.T) {
	_, m := treeApp(t)
	press(m, "l")
	assert.Contains(t, screen(m), "▼ plans")
	assert.Contains(t, screen(m), "Alpha")
	press(m, "j", "l")
	assert.Contains(t, screen(m), "Deep", "j lands on the sub folder, l expands it")
	press(m, "j", "h")
	n := m.view.(*notesView)
	assert.Equal(t, "plans/sub", n.tree.SelectedID(), "h on a note jumps to its folder")
	press(m, "h")
	assert.NotContains(t, screen(m), "Deep", "h on an open folder collapses it")
	press(m, "enter")
	assert.Contains(t, screen(m), "Deep", "enter toggles a folder")
}

func TestFollowingALinkRevealsTheNoteInTheTree(t *testing.T) {
	d, m := treeApp(t)
	press(m, "G", "enter", "tab", "enter")
	s := screen(m)
	assert.Contains(t, s, "› plans/sub/Deep.md")
	assert.Contains(t, s, "▼ sub")
	assert.Equal(t, "plans/sub/Deep.md", m.view.(*notesView).tree.SelectedID())
	write(t, d, "plans/Another.md", "x")
	m.applyReload(vault.Load(d))
	assert.Equal(t, "plans/sub/Deep.md", m.view.(*notesView).tree.SelectedID(), "reload keeps it selected and visible")
}

func TestFilterExpandsMatchesAndEscRestores(t *testing.T) {
	_, m := treeApp(t)
	press(m, "/")
	typeStr(m, "deep")
	s := screen(m)
	assert.Contains(t, s, "Deep")
	assert.NotContains(t, s, "Zeta")
	press(m, "esc")
	s = screen(m)
	assert.Contains(t, s, "▶ plans", "esc restores the collapsed tree")
	assert.Contains(t, s, "Zeta")
}
