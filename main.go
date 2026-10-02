// Command kb is k9s for a Markdown knowledge base: a TUI plus headless search for agents.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/osesantos/kb/internal/config"
	"github.com/osesantos/kb/internal/search"
	"github.com/osesantos/kb/internal/tui"
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

// searchCmd is `kb search [-C vault] [--json] [-n limit] [--budget tokens] words...`: grep-like lines by default, JSON for agents.
func searchCmd(cfg config.Config, args []string) {
	var vaultArg string
	var asJSON bool
	var budget *int
	limit := 20
	words := []string{}
	for i := 0; i < len(args); i++ {
		next := func() string {
			i++
			if i < len(args) {
				return args[i]
			}
			return ""
		}
		switch args[i] {
		case "--json":
			asJSON = true
		case "-C", "--vault":
			vaultArg = next()
		case "-n":
			if n, err := strconv.Atoi(next()); err == nil {
				limit = n
			}
		case "--budget":
			if n, err := strconv.Atoi(next()); err == nil {
				budget = &n
			}
		default:
			words = append(words, args[i])
		}
	}
	v := vault.Load(root(cfg, vaultArg))
	hits := search.Lexical(v, strings.Join(words, " "), limit)
	if asJSON {
		fmt.Println(search.JSON(v, hits, budget))
	} else {
		for _, h := range hits {
			if h.Line > 0 {
				fmt.Printf("%s:%d: %s\n", v.Notes[h.Note].Path, h.Line, h.Snippet)
			} else {
				fmt.Println(v.Notes[h.Note].Path)
			}
		}
	}
	if len(hits) == 0 {
		os.Exit(1)
	}
}

// readCmd is `kb read [-C vault] <note>[#heading]`: prints a note, or only the block under one heading.
func readCmd(cfg config.Config, args []string) {
	var vaultArg, target string
	switch {
	case len(args) == 3 && (args[0] == "-C" || args[0] == "--vault"):
		vaultArg, target = args[1], args[2]
	case len(args) == 1:
		target = args[0]
	default:
		die(2, "usage: kb read [-C vault] <note>[#heading]")
	}
	v := vault.Load(root(cfg, vaultArg))
	i, ok := v.Resolve(target)
	if !ok {
		die(1, "no note %s", target)
	}
	body := v.Notes[i].Body
	_, heading, hasHeading := strings.Cut(target, "#")
	if !hasHeading {
		fmt.Print(body)
		return
	}
	block, ok := vault.Block(body, heading)
	if !ok {
		die(1, "no heading %q in %s", heading, v.Notes[i].Path)
	}
	fmt.Print(block)
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
	case len(args) > 0 && args[0] == "search":
		searchCmd(cfg, args[1:])
	case len(args) > 0 && args[0] == "read":
		readCmd(cfg, args[1:])
	default:
		if err := tui.Run(cfg, vault.Load(root(cfg, first(args)))); err != nil {
			die(1, "%v", err)
		}
	}
}
