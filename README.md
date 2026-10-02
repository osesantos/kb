# kb

k9s for your knowledge base — a fast, keyboard-driven terminal UI to read, navigate, search and git-operate Markdown vaults shared by humans and AI agents.

**Status:** early. `kb [vault]` opens the TUI (notes list, reader, wikilink follow, back/forward history, backlinks, `e` → `$EDITOR`, live refresh on file changes, `:git` status/diff/stage/commit/push, `:activity` commits with `Agent:`/`Run:` trailers, `:cal` daily notes, `:vaults` switch, `s` vault search, `/` find in note). `kb search [-C vault] [--json] [-n N] [--budget TOKENS] words` returns BM25-ranked sections with line ranges; `--budget` inlines section text until the token budget is spent (exit 1 on no hits). `kb read [-C vault] 'note#heading'` prints one section. `kb stats [vault]` prints load timings and link stats.

Install: `cargo install --path . --locked` builds a release binary into `~/.cargo/bin/kb` (on `PATH` wherever rustup is installed); rerun it to update.

Vaults: `~/.config/kb/config.toml` with `[[vault]] name = "main"`, `path = "~/git/vault"`, optional `[vault.daily] folder, format` (defaults come from `.obsidian/daily-notes.json`). A name works wherever a path does; with no argument kb opens `KB_VAULT`, else the first configured vault.
