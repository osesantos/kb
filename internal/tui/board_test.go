package tui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/osesantos/kb/internal/vault"
)

func TestLevelSplitsByQuartileAndBusiestIsFour(t *testing.T) {
	nonzero := []int{1, 2, 3, 4, 10, 100}
	cases := map[int]int{0: 0, 1: 1, 2: 1, 3: 2, 10: 4 - 1, 100: 4}
	for n, want := range cases {
		assert.Equal(t, want, level(n, nonzero), "count %d", n)
	}
	assert.Equal(t, 4, level(5, []int{5}), "a single busy day is the busiest")
}

func TestCountCreatedUsesGitAddsAndUncommittedMtime(t *testing.T) {
	d := t.TempDir()
	gitInit(t, d)
	write(t, d, "old.md", "x")
	gitRun(t, d, "add", "-A")
	gitRun(t, d, "commit", "-q", "-m", "old")
	write(t, d, "new.md", "fresh")
	month := time.Date(time.Now().Year(), time.Now().Month(), 1, 0, 0, 0, 0, time.Local)
	got := countCreated(d, nil, month)
	assert.Equal(t, []string{"new.md", "old.md"}, got[time.Now().Day()])

	plain := t.TempDir()
	write(t, plain, "n.md", "x")
	assert.Equal(t, []string{"n.md"}, countCreated(plain, []string{"n.md"}, month)[time.Now().Day()], "outside git, mtime counts")
}

func TestBoardDrawsWeekdayRowsWeekColumnsAndLegend(t *testing.T) {
	_, m := newApp(t)
	m.board = board{month: time.Date(2026, 10, 1, 0, 0, 0, 0, time.Local), day: 2, loaded: true, focus: true,
		created: map[int][]string{2: {"a.md", "b.md"}, 5: {"c.md"}}}
	lines := strings.Split(ansi.Strip(m.boardView(40)), "\n")
	require.Len(t, lines, 9)
	assert.True(t, strings.HasPrefix(lines[0], "Mon "))
	assert.True(t, strings.HasPrefix(lines[2], "Wed "))
	assert.Equal(t, "Mon   ■ ■ ■ ■", strings.TrimRight(lines[0], " "), "Oct 2026 starts on a Thursday, so week 1 has no Monday")
	assert.Equal(t, "    ■ ■ ■ ■ ■", strings.TrimRight(lines[3], " "), "Thursdays: 1, 8, 15, 22, 29")
	assert.Contains(t, lines[7], "Less")
	assert.Contains(t, lines[8], "Fri 2 Oct · 2 new")
	assert.Equal(t, "October 2026 · 3", m.boardTitle())
}

func boardApp(t *testing.T) (string, *Model) {
	t.Helper()
	d, m := newApp(t)
	today := time.Now()
	write(t, d, ".obsidian/daily-notes.json", `{"folder":"J","format":"YYYY-MM-DD"}`)
	write(t, d, "J/"+today.Format("2006-01-02")+".md", "today's daily")
	m.applyReload(vault.Load(d))
	runCmd(m, m.recount())
	return d, m
}

func TestBoardFocusPreviewKeysAndOpen(t *testing.T) {
	_, m := boardApp(t)
	today := time.Now()
	s := screen(m)
	assert.Contains(t, s, today.Format("January 2006")+" ·")
	press(m, "tab")
	require.True(t, m.board.focus)
	s = screen(m)
	assert.Contains(t, s, today.Format("Monday, 2 January 2006"))
	assert.Contains(t, s, "today's daily")
	assert.Contains(t, s, "notes created")
	press(m, "enter")
	assert.False(t, m.board.focus)
	assert.Contains(t, screen(m), "› J/"+today.Format("2006-01-02")+".md")

	press(m, "esc", "tab", "L")
	assert.NotEqual(t, today.Month(), m.board.month.Month(), "L goes to the next month")
	press(m, "t")
	assert.Equal(t, today.Day(), m.board.day)
	press(m, "tab")
	assert.False(t, m.board.focus)
}

func TestBoardHidesOnSmallTerminalsAndIgnoresOtherVaults(t *testing.T) {
	_, m := boardApp(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	assert.NotContains(t, screen(m), "Less")
	press(m, "tab")
	assert.False(t, m.board.focus)
	before := m.board.created
	m.Update(boardMsg{root: filepath.Join(t.TempDir(), "other"), month: m.board.month, created: map[int][]string{1: {"x"}}})
	assert.Equal(t, before, m.board.created)
}

func TestBoardRecountsOnFileEvents(t *testing.T) {
	d, m := boardApp(t)
	n := m.board.total()
	write(t, d, "brand new.md", "x")
	_, cmd := m.Update(fsMsg{filepath.Join(d, "brand new.md")})
	runCmd(m, cmd)
	assert.Equal(t, n+1, m.board.total())
}

func TestBoardIsCompactAtTheBottomOfTheLeftColumn(t *testing.T) {
	_, m := boardApp(t)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	lines := strings.Split(screen(m), "\n")
	top := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "─ "+time.Now().Format("January 2006")) })
	assert.Equal(t, 40-1-boardHeight, top, "board is the last 12 rows above the help bar, however tall the terminal")
	assert.Len(t, strings.Split(ansi.Strip(m.boardView(40)), "\n"), 9, "no blank lines between weekdays")
}
