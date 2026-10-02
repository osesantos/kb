package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/osesantos/kb/internal/config"
	"github.com/osesantos/kb/internal/vault"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
}

func newApp(t *testing.T) (string, *Model) {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	write(t, d, "A.md", "# A\n\nGo to [[B]].")
	write(t, d, "B.md", "# B\n\nMissing [[Nowhere]].")
	m := New(config.Config{}, vault.Load(d), nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return d, m
}

func keyMsg(s string) tea.KeyPressMsg {
	names := map[string]rune{"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "backspace": tea.KeyBackspace, "up": tea.KeyUp, "down": tea.KeyDown}
	if c, ok := names[s]; ok {
		return tea.KeyPressMsg(tea.Key{Code: c})
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg(tea.Key{Code: r, Text: s})
}

func press(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(keyMsg(k))
	}
}

func typeStr(m *Model, s string) {
	for _, r := range s {
		press(m, string(r))
	}
}

func screen(m *Model) string { return ansi.Strip(m.View().Content) }

func TestFilterOpenFollowAndHistory(t *testing.T) {
	_, m := newApp(t)
	assert.Contains(t, screen(m), "notes")
	press(m, "/", "a", "enter", "enter")
	s := screen(m)
	assert.Contains(t, s, "A.md")
	assert.Contains(t, s, "Go to")
	press(m, "tab")
	assert.Contains(t, screen(m), "Links")
	press(m, "enter")
	assert.Contains(t, screen(m), "B.md")
	press(m, "[")
	assert.Contains(t, screen(m), "A.md")
	press(m, "]")
	assert.Contains(t, screen(m), "Backlinks (1)", "backlinks section missing")
}

func TestUnresolvedLinkReportsStatus(t *testing.T) {
	_, m := newApp(t)
	press(m, "j", "enter", "tab")
	assert.Contains(t, screen(m), "Nowhere")
	press(m, "enter")
	assert.Contains(t, screen(m), "unresolved: Nowhere")
}

func TestArrowsMoveLikeJK(t *testing.T) {
	_, m := newApp(t)
	press(m, "down", "enter")
	assert.Contains(t, screen(m), "B.md")
	press(m, "esc", "up", "enter")
	assert.Contains(t, screen(m), "A.md")
}

func TestEditRequestsEditorForCurrentNote(t *testing.T) {
	d, m := newApp(t)
	assert.NotNil(t, m.view.key(m, "e"))
	assert.Equal(t, filepath.Join(d, "A.md"), m.editing)
	press(m, "down", "enter")
	assert.NotNil(t, m.view.key(m, "e"))
	assert.Equal(t, filepath.Join(d, "B.md"), m.editing)
}

func TestReloadKeepsReaderHistoryAndPicksUpChanges(t *testing.T) {
	d, m := newApp(t)
	press(m, "enter", "tab", "enter")
	write(t, d, "B.md", "# B\n\nEdited in nvim.")
	write(t, d, "0 New.md", "new")
	m.applyReload(vault.Load(d))
	s := screen(m)
	assert.Contains(t, s, "B.md")
	assert.Contains(t, s, "Edited in nvim.")
	press(m, "[")
	assert.Contains(t, screen(m), "A.md")
	press(m, "esc")
	assert.Contains(t, screen(m), "0 New")
}

func TestReloadClosesReaderOfDeletedNote(t *testing.T) {
	d, m := newApp(t)
	press(m, "enter")
	require.NoError(t, os.Remove(filepath.Join(d, "A.md")))
	m.applyReload(vault.Load(d))
	s := screen(m)
	assert.Contains(t, s, "A.md was removed")
	assert.False(t, m.right)
}

func TestUnknownCommandAndQuit(t *testing.T) {
	_, m := newApp(t)
	press(m, ":")
	typeStr(m, "nope")
	press(m, "enter")
	assert.Contains(t, screen(m), "unknown command: nope")
	press(m, ":")
	typeStr(m, "q")
	_, cmd := m.Update(keyMsg("enter"))
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestTooSmallTerminal(t *testing.T) {
	_, m := newApp(t)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 8})
	assert.Contains(t, screen(m), "Terminal too small")
}

func TestQuitFromList(t *testing.T) {
	_, m := newApp(t)
	_, cmd := m.Update(keyMsg("q"))
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestWikilinksBecomeTextOutsideFences(t *testing.T) {
	got := wikilinksToText("see [[B#H|the B]] and ![[pic.png]]\n```\n[[code]]\n```")
	assert.Equal(t, "see **the B** and *⧉ pic.png*\n```\n[[code]]\n```", got)
	assert.True(t, strings.Contains(wikilinksToText("[[X]]"), "**X**"))
}
