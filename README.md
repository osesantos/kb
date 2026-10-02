# kb

k9s for your knowledge base — a fast, keyboard-driven terminal UI to read, navigate, search and git-operate Markdown vaults shared by humans and AI agents. Go + Bubble Tea, styled like [Overseer](https://github.com/dnlopes/overseer).

## Install

`make install` builds `kb` into `~/.local/bin` (set `BIN=` to change it). Needs Go 1.25+ (`GOTOOLCHAIN=auto` fetches it).

## TUI

`kb [vault]` opens a notes tree (folders first, A→Z) with a GitHub-style board of notes created per day this month under it, and a preview on the right. Created dates come from the commit that first added each note; uncommitted new notes count on the day they were saved.

| Key / command | What it does |
|---|---|
| `j/k`, `⏎`, `esc` | move, open / focus preview, back |
| `l` `h` | notes tree: open / close a folder (or jump to its parent) |
| `tab` | switch between the notes tree and the contribution board (`j/k` day, `h/l` week, `H/L` month, `t` today, `⏎` daily note) |
| `/` | filter notes by path (list) or find in note (preview, then `n`/`N`) |
| `tab` (in a note) | link picker: the note's links, embeds and backlinks |
| `[` `]` | history back / forward |
| `e` | edit in `$VISUAL`/`$EDITOR`; the view reloads on save and on any external change |
| `s`, `:s words` | BM25 search over sections |
| `:git` | status, diff, `space` stage, `a` all, `c` commit, `p` push, `A` auto-commit (stage all, commit "auto-commit: <date time>", push) |
| `:activity` | commits touching the vault, attributed by `Agent:` / `Run:` trailers |
| `:cal` | daily notes for the month (`H/L` month, `t` today, `e` creates) |
| `:vaults` | switch between configured vaults |
| `:notes`, `:q` | back to notes, quit |

## CLI

- `kb search [-C vault] [--json] [-n N] [--budget TOKENS] words` — BM25-ranked `##`/`###` sections with line ranges. `--budget` inlines section text until the token budget is spent. Exit 1 on no hits.
- `kb read [-C vault] 'note#heading'` — prints one section.
- `kb stats [vault]` — load time and link stats.

## Config

`~/.config/kb/config.toml`:

```toml
theme = "dark"   # dark dracula github-dark tokyo-night monokai one-dark solarized-dark nord catppuccin-mocha porcelain deep-sea sunset

[[vault]]
name = "main"
path = "~/git/vault"

[vault.daily]            # optional; defaults come from .obsidian/daily-notes.json
folder = "Journals"
format = "MMM Do, YYYY"
```

A vault name works wherever a path does. With no argument kb opens `KB_VAULT`, else the first configured vault, else the current directory.

## Develop

`make test` runs vet and the tests. `KB_VAULT=~/git/vault go test ./internal/search -run Budgets -v` checks the load and query budgets against a real vault.

`internal/tui/styles`, `internal/tui/components` and `markdown.go` are copied from Overseer (MIT) — see `NOTICE`.
