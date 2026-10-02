package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/osesantos/kb/internal/config"
	"github.com/osesantos/kb/internal/vault"
)

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}, {"config", "commit.gpgsign", "false"}} {
		out, err := exec.Command("git", append([]string{"-C", dir}, a...)...).CombinedOutput()
		require.NoError(t, err, string(out))
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	require.NoError(t, err, string(out))
}

func command(m *Model, c string) {
	press(m, ":")
	typeStr(m, c)
	press(m, "enter")
}

func TestSearchOpensNoteAtFirstMatchAndBackReturnsToResults(t *testing.T) {
	_, m := newApp(t)
	press(m, "s")
	typeStr(m, "nowhere")
	press(m, "enter")
	s := screen(m)
	assert.Contains(t, s, `search "nowhere" (1)`)
	assert.Contains(t, s, "B.md:3")
	press(m, "enter")
	s = screen(m)
	assert.Contains(t, s, "match 1/1")
	press(m, "esc", "esc")
	assert.Contains(t, screen(m), `search "nowhere"`)
	command(m, "s zzz")
	assert.Contains(t, screen(m), "no matches")
}

func TestFindInNoteCyclesMatches(t *testing.T) {
	d, m := newApp(t)
	write(t, d, "A.md", "one x\n\ntwo x\n\nthree x")
	m.applyReload(vault.Load(d))
	press(m, "enter", "/")
	typeStr(m, "X")
	press(m, "enter")
	assert.Contains(t, screen(m), "match 1/3")
	press(m, "N")
	assert.Contains(t, screen(m), "match 3/3")
	press(m, "esc")
	assert.Contains(t, screen(m), "find")
	assert.Empty(t, m.find.query)
}

func TestGitViewStageDiffCommit(t *testing.T) {
	d, m := newApp(t)
	gitInit(t, d)
	m.dirty, m.dirtyOK = 2, true
	assert.Contains(t, screen(m), "±2")
	command(m, "git")
	s := screen(m)
	assert.Contains(t, s, "?? A.md")
	assert.Contains(t, s, "+Go to")
	press(m, "space")
	assert.Contains(t, screen(m), "A  A.md")
	press(m, "a", "c")
	typeStr(m, "notes")
	press(m, "enter")
	s = screen(m)
	assert.Contains(t, s, "committed")
	assert.Contains(t, s, "working tree clean")
	press(m, "esc")
	assert.Contains(t, screen(m), "Notes")
}

func TestGitOutsideRepo(t *testing.T) {
	_, m := newApp(t)
	command(m, "git")
	assert.Contains(t, screen(m), "not a git repository")
}

func TestActivityAttributesTrailersAndShowsCommit(t *testing.T) {
	d, m := newApp(t)
	gitInit(t, d)
	gitRun(t, d, "add", "-A")
	gitRun(t, d, "commit", "-q", "-m", "human edit")
	write(t, d, "A.md", "agent wrote this")
	gitRun(t, d, "add", "-A")
	gitRun(t, d, "commit", "-q", "-m", "Research: x\n\nAgent: overseer\nRun: r7")
	command(m, "activity")
	s := screen(m)
	assert.Contains(t, s, "overseer · r7")
	assert.Contains(t, s, "external")
	assert.Contains(t, s, "+agent wrote this")
	press(m, "esc")
	assert.Contains(t, screen(m), "Notes")
}

func TestCalOpensTodayFromObsidianConventionAndOffersCreate(t *testing.T) {
	d, m := newApp(t)
	today := time.Now()
	write(t, d, ".obsidian/daily-notes.json", `{"folder":"J","format":"YYYY-MM-DD"}`)
	write(t, d, "J/"+today.Format("2006-01-02")+".md", "today note")
	m.applyReload(vault.Load(d))
	command(m, "cal")
	s := screen(m)
	assert.Contains(t, s, today.Format("January 2006"))
	assert.Contains(t, s, "today note")
	assert.Regexp(t, today.Format("02")+`\s+note`, s, "days with a note are marked")
	assert.NotRegexp(t, today.AddDate(0, 0, 1).Format("02")+`\s+note`, s)
	press(m, "enter")
	assert.Contains(t, screen(m), "J/"+today.Format("2006-01-02")+".md")
	press(m, "esc")
	assert.Contains(t, screen(m), today.Format("January 2006"), "esc returns to the calendar")
	tomorrow := today.AddDate(0, 0, 1)
	if tomorrow.Month() != today.Month() {
		t.Skip("last day of the month")
	}
	press(m, "j", "enter")
	assert.Contains(t, screen(m), "no daily note")
	press(m, "e")
	assert.Equal(t, filepath.Join(d, "J", tomorrow.Format("2006-01-02")+".md"), m.editing)
}

func TestVaultsListsConfigAndSwitches(t *testing.T) {
	d, _ := newApp(t)
	other, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(other, "Z.md"), []byte("z"), 0o644))
	cfg := config.Config{Vaults: []config.VaultCfg{{Name: "one", Path: d}, {Name: "two", Path: other}}}
	m := New(cfg, vault.Load(d), nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	command(m, "vaults")
	s := screen(m)
	assert.Contains(t, s, "vaults (2)")
	assert.Contains(t, s, "● one")
	assert.Contains(t, s, "two")
	press(m, "down")
	_, cmd := m.Update(keyMsg("enter"))
	require.NotNil(t, cmd)
	m.Update(cmd())
	t.Cleanup(func() { _ = m.watch.Close() })
	s = screen(m)
	assert.Contains(t, s, "switched to two")
	assert.Equal(t, other, m.v.Root)
	assert.Contains(t, s, "Z")
}

func vaultsApp(t *testing.T) (string, string, *Model) {
	t.Helper()
	d, m := newApp(t)
	m.cfgPath = filepath.Join(t.TempDir(), "kb", "config.toml")
	other, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	command(m, "vaults")
	return d, other, m
}

func TestNewVaultFormAddsToConfigAndList(t *testing.T) {
	_, other, m := vaultsApp(t)
	assert.Contains(t, screen(m), "press n to add one")
	press(m, "n")
	assert.Contains(t, screen(m), "New Vault")
	typeStr(m, "work")
	press(m, "tab", "ctrl+u")
	typeStr(m, other)
	press(m, "enter")
	s := screen(m)
	assert.Contains(t, s, "added vault work")
	assert.Contains(t, s, "vaults (1)")
	assert.Nil(t, m.form)
	b, err := os.ReadFile(m.cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(b), `name = "work"`)
	assert.Contains(t, string(b), other)
	c, err := config.Load(m.cfgPath)
	require.NoError(t, err)
	assert.Equal(t, other, c.Vaults[0].Path)
}

func TestNewVaultFormValidatesAndCancels(t *testing.T) {
	_, other, m := vaultsApp(t)
	press(m, "n", "tab", "ctrl+u")
	typeStr(m, "/definitely/not/here")
	press(m, "enter")
	assert.Contains(t, screen(m), "not a directory")
	assert.NotNil(t, m.form)
	press(m, "ctrl+u")
	typeStr(m, other)
	press(m, "enter")
	assert.Nil(t, m.form)
	assert.Equal(t, filepath.Base(other), m.cfg.Vaults[0].Name, "empty name falls back to the folder name")

	press(m, "n", "tab", "ctrl+u")
	typeStr(m, other)
	press(m, "enter")
	assert.Contains(t, screen(m), "already")
	press(m, "esc")
	assert.Nil(t, m.form)
	assert.Len(t, m.cfg.Vaults, 1)
}
