// Command kb is k9s for a Markdown knowledge base: a TUI plus headless search for agents.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/osesantos/kb/internal/config"
	"github.com/osesantos/kb/internal/vault"
)

func die(code int, format string, a ...any) {
	fmt.Fprintf(os.Stderr, "kb: "+format+"\n", a...)
	os.Exit(code)
}

// root picks the vault: the argument, else KB_VAULT, else the first configured vault, else "."; names resolve via config.
func root(cfg config.Config, arg string) string {
	if arg == "" {
		arg = os.Getenv("KB_VAULT")
	}
	switch {
	case arg != "":
		return cfg.Root(arg)
	case len(cfg.Vaults) > 0:
		return cfg.Vaults[0].Path
	default:
		return cfg.Root(".")
	}
}

func first(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func stats(dir string) {
	t := time.Now()
	v := vault.Load(dir)
	took := time.Since(t)
	links, unresolved := 0, 0
	for _, n := range v.Notes {
		for _, l := range n.Links {
			links++
			if v.Target(l).Kind == vault.Missing {
				unresolved++
			}
		}
	}
	fmt.Printf("notes       %d\nload        %v\nwikilinks   %d\nunresolved  %d\n", len(v.Notes), took, links, unresolved)
}

func main() {
	cfg, err := config.Load(config.Path())
	if err != nil {
		die(2, "%v", err)
	}
	args := os.Args[1:]
	switch {
	case len(args) > 0 && args[0] == "stats":
		stats(root(cfg, first(args[1:])))
	default:
		die(2, "the TUI is not ported yet")
	}
}
