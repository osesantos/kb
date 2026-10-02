package vault

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func next(t *testing.T, w *Watcher) []string {
	t.Helper()
	select {
	case b := <-w.Batches:
		return b
	case <-time.After(2 * time.Second):
		t.Fatal("no batch")
		return nil
	}
}

func TestWatchBatchesChangesInNewNestedDirs(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	w, err := Watch(dir, 100*time.Millisecond)
	require.NoError(t, err)
	defer w.Close()

	nested := filepath.Join(dir, "a", "b")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	time.Sleep(300 * time.Millisecond)
	next(t, w)

	require.NoError(t, os.WriteFile(filepath.Join(nested, "n.md"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(nested, "m.md"), []byte("y"), 0o644))
	got := next(t, w)
	assert.Contains(t, got, filepath.Join(nested, "n.md"))
	assert.Contains(t, got, filepath.Join(nested, "m.md"))
}

func TestRelevantIgnoresGitAndObsidian(t *testing.T) {
	assert.False(t, Relevant("/v/.git/index"))
	assert.False(t, Relevant("/v/.obsidian/workspace.json"))
	assert.True(t, Relevant("/v/Notes/x.md"))
	assert.True(t, InGit("/v/.git/index"))
	assert.False(t, InGit("/v/Notes/x.md"))
}
