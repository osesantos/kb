package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ensureDir creates the parent directory of path so a new note can be saved from the editor.
func ensureDir(path string) error { return os.MkdirAll(filepath.Dir(path), 0o755) }

func truncate(s string, w int) string { return ansi.Truncate(s, max(w, 1), "…") }

// truncateLines cuts every line of s to w cells so a narrow panel never wraps.
func truncateLines(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = truncate(l, w)
	}
	return strings.Join(lines, "\n")
}
