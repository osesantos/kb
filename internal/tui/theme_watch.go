package tui

import (
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/fsnotify/fsnotify"

	"github.com/osesantos/kb/internal/tui/styles"
)

// omarchyDir is where the active Omarchy theme lives; tests blank it to stay hermetic.
var omarchyDir = styles.OmarchyDir()

type themeMsg struct{}

// watchTheme signals each Omarchy theme switch under dir; nil when dir cannot be watched.
// It watches the parent because omarchy-theme-set replaces the theme directory wholesale.
func watchTheme(dir string) <-chan struct{} {
	if dir == "" {
		return nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil
	}
	if w.Add(dir) != nil {
		_ = w.Close()
		return nil
	}
	ch := make(chan struct{}, 1)
	go func() {
		var t *time.Timer
		for ev := range w.Events {
			if b := filepath.Base(ev.Name); b != "theme" && b != "theme.name" {
				continue
			}
			if t != nil {
				t.Stop()
			}
			t = time.AfterFunc(200*time.Millisecond, func() {
				select {
				case ch <- struct{}{}:
				default:
				}
			})
		}
	}()
	return ch
}

func (m *Model) waitTheme() tea.Cmd {
	if m.themes == nil {
		return nil
	}
	ch := m.themes
	return func() tea.Msg { <-ch; return themeMsg{} }
}
