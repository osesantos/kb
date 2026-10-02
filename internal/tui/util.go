package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func envOr(k string) string { return os.Getenv(k) }

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// ensureDir creates the parent directory of path so a new note can be saved from the editor.
func ensureDir(path string) error { return os.MkdirAll(filepath.Dir(path), 0o755) }

func truncate(s string, w int) string { return ansi.Truncate(s, max(w, 1), "…") }

func splitLines(s string) []string { return strings.Split(s, "\n") }
