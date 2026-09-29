# kb

k9s for your knowledge base — a fast, keyboard-driven terminal UI to read, navigate, search and git-operate Markdown vaults shared by humans and AI agents.

**Status:** early. `kb [vault]` opens the TUI (notes list, reader, wikilink follow, back/forward history, backlinks, `e` → `$EDITOR`, live refresh on file changes, `:git` status/diff/stage/commit/push, `s` vault search, `/` find in note). `kb search [-C vault] [--json] [-n N] words` searches from the shell (exit 1 on no hits; `KB_VAULT` sets the default vault). `kb stats [vault]` prints load timings and link stats.
