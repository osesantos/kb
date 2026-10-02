package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/osesantos/kb/internal/vault"
)

func TestReloadRemapsSearchResultsWaitingBehindAnOpenHit(t *testing.T) {
	d, m := newApp(t)
	press(m, "s")
	typeStr(m, "nowhere")
	press(m, "enter", "enter")
	write(t, d, "0 first.md", "sorts before A and B")
	m.applyReload(vault.Load(d))
	assert.NotPanics(t, func() { press(m, "esc", "esc") })
	s := screen(m)
	assert.Contains(t, s, "B.md:3")
	assert.NotContains(t, s, "A.md:")
}

func TestReloadRebuildsOpenLinkPicker(t *testing.T) {
	d, m := newApp(t)
	press(m, "enter", "tab")
	require.NotNil(t, m.picker)
	write(t, d, "0 first.md", "x")
	m.applyReload(vault.Load(d))
	require.NotNil(t, m.picker)
	press(m, "enter")
	assert.Contains(t, screen(m), "› B.md")
}

func TestFindIsClearedWhenAnotherNoteIsShown(t *testing.T) {
	_, m := newApp(t)
	press(m, "enter", "/")
	typeStr(m, "go")
	press(m, "enter")
	require.NotEmpty(t, m.find.query)
	press(m, "tab", "enter")
	assert.Contains(t, screen(m), "› B.md")
	assert.Empty(t, m.find.query)
	assert.NotContains(t, screen(m), "match ")
}

func TestResultsFromAnotherVaultAreIgnored(t *testing.T) {
	d, m := newApp(t)
	other := t.TempDir()
	write(t, other, "Z.md", "z")
	m.Update(reloadedMsg{vault.Load(other)})
	m.Update(dirtyMsg{root: other, n: 9, ok: true})
	assert.Equal(t, d, m.v.Root)
	assert.NotContains(t, screen(m), "±9")
}

func TestNewVaultFormStoresRelativePathsAsAbsolute(t *testing.T) {
	_, other, m := vaultsApp(t)
	t.Chdir(filepath.Dir(other))
	press(m, "n", "tab", "ctrl+u")
	typeStr(m, filepath.Base(other))
	press(m, "enter")
	b, err := os.ReadFile(m.cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(b), `path = "`+other+`"`)
}

func TestHistoryKeepsPointingAtTheSameNoteWhenAnEarlierEntryIsDeleted(t *testing.T) {
	d, m := newApp(t)
	write(t, d, "C.md", "c")
	m.applyReload(vault.Load(d))
	m.hist, m.pos = []string{"A.md", "B.md", "C.md"}, 1
	require.NoError(t, os.Remove(filepath.Join(d, "A.md")))
	m.applyReload(vault.Load(d))
	assert.Equal(t, "B.md", m.hist[m.pos])
}
