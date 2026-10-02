package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

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

// runCmd executes a command and feeds every resulting message back into the model, like the Bubble Tea runtime.
func runCmd(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runCmd(m, c)
		}
	case nil:
	default:
		_, next := m.Update(msg)
		runCmd(m, next)
	}
}

func TestEditorSaveRefreshesThePreview(t *testing.T) {
	d, m := newApp(t)
	press(m, "enter")
	require.NotNil(t, m.view.key(m, "e"))
	write(t, d, "A.md", "# A\n\nsaved from the editor")
	_, cmd := m.Update(editedMsg{})
	runCmd(m, cmd)
	s := screen(m)
	assert.Contains(t, s, "saved from the editor")
	assert.Contains(t, s, "› A.md")
}

func TestActivityRefreshesOnlyOnGitChanges(t *testing.T) {
	d, m := newApp(t)
	gitInit(t, d)
	gitRun(t, d, "add", "-A")
	gitRun(t, d, "commit", "-q", "-m", "first")
	command(m, "activity")
	write(t, d, "B.md", "changed")
	gitRun(t, d, "commit", "-q", "-am", "second")
	m.Update(fsMsg{filepath.Join(d, "A.md")})
	assert.NotContains(t, screen(m), "second", "a note save does not re-read git history")
	m.Update(fsMsg{filepath.Join(d, ".git", "index")})
	assert.Contains(t, screen(m), "second")
}

func TestAutoCommitStagesCommitsAndPushes(t *testing.T) {
	d, m := newApp(t)
	remote := t.TempDir()
	gitRun(t, remote, "init", "-q", "--bare")
	gitInit(t, d)
	gitRun(t, d, "remote", "add", "origin", remote)
	gitRun(t, d, "config", "push.autoSetupRemote", "true")
	command(m, "git")
	_, cmd := m.Update(keyMsg("A"))
	require.NotNil(t, cmd)
	assert.Contains(t, screen(m), "working tree clean")
	runCmd(m, cmd)
	assert.Contains(t, screen(m), "pushed")
	out, err := exec.Command("git", "-C", remote, "log", "-1", "--format=%s").Output()
	require.NoError(t, err)
	assert.Regexp(t, `^auto-commit: \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\n$`, string(out))

	_, cmd = m.Update(keyMsg("A"))
	assert.Nil(t, cmd)
	assert.Contains(t, screen(m), "nothing to commit")
}
