package tui

import (
	"os"
	"path/filepath"

	"github.com/charmbracelet/x/ansi"
)

// ensureDir creates the parent directory of path so a new note can be saved from the editor.
func ensureDir(path string) error { return os.MkdirAll(filepath.Dir(path), 0o755) }

func truncate(s string, w int) string { return ansi.Truncate(s, max(w, 1), "…") }
