package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	omarchyDir = ""
	os.Exit(m.Run())
}

func TestWatchThemeSeesOmarchySwap(t *testing.T) {
	dir := t.TempDir()
	ch := watchTheme(dir)
	if ch == nil {
		t.Fatal("watch failed")
	}
	next := filepath.Join(dir, "next-theme")
	_ = os.Mkdir(next, 0o755)
	_ = os.Rename(next, filepath.Join(dir, "theme"))
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("theme swap not seen")
	}
}
