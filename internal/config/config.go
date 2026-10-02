// Package config reads ~/.config/kb/config.toml: the vaults kb knows about, their overrides and the theme.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Vaults []VaultCfg `toml:"vault"`
	// Theme names an Overseer palette; empty means the default.
	Theme string `toml:"theme"`
}

type VaultCfg struct {
	Name  string   `toml:"name"`
	Path  string   `toml:"path"`
	Daily DailyCfg `toml:"daily"`
}

// DailyCfg holds daily-note overrides; an empty field falls back to the vault's Obsidian settings.
type DailyCfg struct {
	Folder string `toml:"folder"`
	Format string `toml:"format"`
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// Expand resolves a leading `~` and symlinks, so configured paths compare equal to the opened root.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(home(), p[1:])
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return p
	}
	return real
}

// Path is $XDG_CONFIG_HOME/kb/config.toml, defaulting to ~/.config.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home(), ".config")
	}
	return filepath.Join(base, "kb", "config.toml")
}

// Load parses the config file; a missing file is an empty config, a malformed one is an error.
func Load(file string) (Config, error) {
	var c Config
	src, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("%s: %w", file, err)
	}
	if err := toml.Unmarshal(src, &c); err != nil {
		return c, fmt.Errorf("%s: %w", file, err)
	}
	for i := range c.Vaults {
		c.Vaults[i].Path = Expand(c.Vaults[i].Path)
	}
	return c, nil
}

// Root is a configured vault's path by name, else arg taken as a path.
func (c Config) Root(arg string) string {
	for _, v := range c.Vaults {
		if v.Name == arg {
			return v.Path
		}
	}
	return Expand(arg)
}

// Daily is the daily-note override for the vault at root, empty when none is configured.
func (c Config) Daily(root string) DailyCfg {
	for _, v := range c.Vaults {
		if v.Path == root {
			return v.Daily
		}
	}
	return DailyCfg{}
}
