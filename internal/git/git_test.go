package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Init makes dir an unsigned test repository.
func Init(t *testing.T, dir string) {
	t.Helper()
	for _, a := range [][]string{{"init", "-q"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}, {"config", "commit.gpgsign", "false"}} {
		_, err := run(dir, []int{0}, a...)
		require.NoError(t, err)
	}
}

func TestParsesPorcelainZIncludingRenames(t *testing.T) {
	e := Parse(" M a b.md\x00R  new.md\x00old.md\x00?? x.md\x00")
	assert.Equal(t, []Entry{{' ', 'M', "a b.md"}, {'R', ' ', "new.md"}, {'?', '?', "x.md"}}, e)
	assert.True(t, e[1].Staged())
	assert.False(t, e[0].Staged())
	assert.True(t, e[2].Untracked())
}

func TestStageCommitCycle(t *testing.T) {
	d := t.TempDir()
	Init(t, d)
	require.NoError(t, os.WriteFile(filepath.Join(d, "n.md"), []byte("hi\n"), 0o644))
	st, err := Status(d)
	require.NoError(t, err)
	e := st[0]
	assert.True(t, e.Untracked())
	diff, err := Diff(d, e)
	require.NoError(t, err)
	assert.Contains(t, diff, "+hi")
	_, err = Stage(d, e.Path)
	require.NoError(t, err)
	st, err = Status(d)
	require.NoError(t, err)
	assert.True(t, st[0].Staged())
	_, err = Unstage(d, "n.md")
	require.NoError(t, err)
	_, err = StageAll(d)
	require.NoError(t, err)
	out, err := Commit(d, "first")
	require.NoError(t, err)
	assert.Regexp(t, `first\n$`, out)
	st, err = Status(d)
	require.NoError(t, err)
	assert.Empty(t, st)
	_, err = Push(d)
	assert.Error(t, err)
}

func TestLogReadsTrailersAndScopesToRoot(t *testing.T) {
	d := t.TempDir()
	Init(t, d)
	l, err := Log(d, 10)
	require.NoError(t, err)
	assert.Empty(t, l)
	v := filepath.Join(d, "v")
	require.NoError(t, os.Mkdir(v, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(v, "n.md"), []byte("hi\n"), 0o644))
	_, err = StageAll(d)
	require.NoError(t, err)
	_, err = run(d, []int{0}, "commit", "-q", "-m", "Research: x\n\nbody\n\nAgent: overseer/triage\nRun: r1")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(d, "out.md"), []byte("x"), 0o644))
	_, err = StageAll(d)
	require.NoError(t, err)
	_, err = Commit(d, "outside the vault")
	require.NoError(t, err)

	c, err := Log(v, 10)
	require.NoError(t, err)
	require.Len(t, c, 1)
	assert.Equal(t, []string{"overseer/triage", "r1", "Research: x"}, []string{c[0].Agent, c[0].Run, c[0].Subject})
	show, err := Show(v, c[0].Hash)
	require.NoError(t, err)
	assert.Contains(t, show, "+hi")
	all, err := Log(d, 10)
	require.NoError(t, err)
	assert.Empty(t, all[0].Agent)
	_, err = Log(t.TempDir(), 10)
	assert.ErrorContains(t, err, "not a git repository")
}

func TestCreatedReturnsFirstAddDatesInRange(t *testing.T) {
	d := t.TempDir()
	Init(t, d)
	commitAt := func(when string, files ...string) {
		t.Helper()
		for _, f := range files {
			require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(d, f)), 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(d, f), []byte(when), 0o644))
		}
		_, err := StageAll(d)
		require.NoError(t, err)
		cmd := exec.Command("git", "-C", d, "commit", "-q", "-m", when)
		cmd.Env = append(os.Environ(), "GIT_COMMITTER_DATE="+when, "GIT_AUTHOR_DATE="+when)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
	commitAt("2026-09-30T12:00:00Z", "old.md")
	commitAt("2026-10-02T12:00:00Z", "a b.md", "dir/c.md", "pic.png")
	commitAt("2026-10-05T12:00:00Z", "old.md", "d.md", "🤩 Feedzai/ção.md")

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got, err := Created(d, from, from.AddDate(0, 1, 0))
	require.NoError(t, err)
	assert.Len(t, got, 4, "old.md was edited, not added, in October; png is not a note")
	assert.Equal(t, 2, got["a b.md"].UTC().Day())
	assert.Equal(t, 2, got["dir/c.md"].UTC().Day())
	assert.Equal(t, 5, got["d.md"].UTC().Day())
	assert.Equal(t, 5, got["🤩 Feedzai/ção.md"].UTC().Day(), "non-ASCII paths are not quoted")

	_, err = Created(t.TempDir(), from, from)
	assert.ErrorContains(t, err, "not a git repository")
}
