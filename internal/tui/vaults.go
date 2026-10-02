package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/config"
	"github.com/osesantos/kb/internal/vault"
)

// vaultsView lists the configured vaults; Enter switches to one.
type vaultsView struct{ selIdx int }

func newVaults(m *Model) *vaultsView {
	v := &vaultsView{}
	for i, c := range m.cfg.Vaults {
		if c.Path == m.v.Root {
			v.selIdx = i
		}
	}
	return v
}

func (v *vaultsView) crumb(m *Model) string { return fmt.Sprintf("vaults (%d)", len(m.cfg.Vaults)) }
func (v *vaultsView) listTitle() string     { return "Vaults" }
func (v *vaultsView) sel() int              { return v.selIdx }
func (v *vaultsView) move(m *Model, d int) {
	v.selIdx = max(min(v.selIdx+d, len(m.cfg.Vaults)-1), 0)
}

func (v *vaultsView) rows(m *Model) []row {
	rows := make([]row, len(m.cfg.Vaults))
	for i, c := range m.cfg.Vaults {
		mark := "  "
		if c.Path == m.v.Root {
			mark = m.st.Glyphs.Today + " "
		}
		rows[i] = row{text: mark + c.Name, aux: c.Path}
	}
	return rows
}

func (v *vaultsView) cfg(m *Model) (config.VaultCfg, bool) {
	if v.selIdx < 0 || v.selIdx >= len(m.cfg.Vaults) {
		return config.VaultCfg{}, false
	}
	return m.cfg.Vaults[v.selIdx], true
}

func (v *vaultsView) preview(m *Model) (string, string, func(int) string) {
	c, ok := v.cfg(m)
	if !ok {
		return "Vault", "none", func(int) string {
			return m.st.Empty.Hint.Render("no vaults yet — press n to add one")
		}
	}
	return c.Name, c.Path, func(int) string {
		s := m.st
		cur := ""
		if c.Path == m.v.Root {
			cur = "\n" + s.Toast.Good.Render("currently open")
		}
		return s.Group.Header.Render(c.Name) + cur + "\n\n" +
			s.Empty.Title.Render("Path  ") + c.Path + "\n" +
			s.Empty.Title.Render("Daily folder  ") + orDefault(c.Daily.Folder) + "\n" +
			s.Empty.Title.Render("Daily format  ") + orDefault(c.Daily.Format)
	}
}

func orDefault(s string) string {
	if s == "" {
		return "from .obsidian/daily-notes.json"
	}
	return s
}

func (v *vaultsView) key(m *Model, k string) tea.Cmd {
	if m.moveSel(&v.selIdx, len(m.cfg.Vaults), k) {
		return nil
	}
	switch k {
	case "esc", "q":
		m.backToNotes()
	case "n":
		m.form = newVaultForm(m)
	case "enter":
		if c, ok := v.cfg(m); ok {
			return switchVault(c)
		}
	}
	return nil
}

// switchVault loads the vault and starts its watcher off the UI goroutine.
func switchVault(c config.VaultCfg) tea.Cmd {
	return func() tea.Msg {
		v := vault.Load(c.Path)
		w, err := vault.Watch(c.Path, 200*time.Millisecond)
		return switchedMsg{name: c.Name, v: v, w: w, err: err}
	}
}

func (v *vaultsView) help(bool) []hint {
	return []hint{{"j/k", "move"}, {"⏎", "switch"}, {"n", "new vault"}, {"esc", "notes"}}
}
