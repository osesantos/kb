package tui

import (
	"fmt"
	"image/color"
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

// onBackground paints every line of s on bg, padded to width: the colour is re-applied after each SGR reset,
// because styled spans (glamour, lipgloss) end with a reset that would otherwise fall back to the terminal's own background.
func onBackground(s string, bg color.Color, width int) string {
	r, g, b, _ := bg.RGBA()
	open := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r>>8, g>>8, b>>8)
	fix := strings.NewReplacer("\x1b[0m", "\x1b[0m"+open, "\x1b[m", "\x1b[m"+open)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		l = truncate(l, width)
		lines[i] = open + fix.Replace(l) + strings.Repeat(" ", max(width-ansi.StringWidth(l), 0)) + "\x1b[m"
	}
	return strings.Join(lines, "\n")
}
