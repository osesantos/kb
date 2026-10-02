package styles

// Glyphs are the small symbols used in lists; the plain set avoids emoji-width surprises in some terminals.
type Glyphs struct {
	Note, Folder, Link, Backlink, Dirty, Agent, External, Today, Missing, Cursor string
}

func NewGlyphs(plain bool) Glyphs {
	if plain {
		return Glyphs{Note: "·", Folder: "▸", Link: "→", Backlink: "←", Dirty: "±", Agent: "◆", External: "○", Today: "●", Missing: "×", Cursor: "▌"}
	}
	return Glyphs{Note: "󰈙", Folder: "", Link: "→", Backlink: "←", Dirty: "±", Agent: "◆", External: "○", Today: "●", Missing: "×", Cursor: "▌"}
}
