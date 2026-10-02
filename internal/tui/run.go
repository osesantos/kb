package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/osesantos/kb/internal/config"
	"github.com/osesantos/kb/internal/vault"
)

// Run opens the TUI on a vault and blocks until the user quits.
func Run(cfg config.Config, v *vault.Vault) error {
	w, err := vault.Watch(v.Root, 200*time.Millisecond)
	if err != nil {
		return fmt.Errorf("watch %s: %w", v.Root, err)
	}
	defer w.Close()
	_, err = tea.NewProgram(New(cfg, v, w)).Run()
	return err
}
