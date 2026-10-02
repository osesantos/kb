package styles

// Glyphs are the small symbols used in lists and the titlebar.
type Glyphs struct {
	Link, Backlink, Dirty, Today, Missing string
}

// NewGlyphs returns the plain glyph set; it needs no Nerd Font.
func NewGlyphs() Glyphs {
	return Glyphs{Link: "→", Backlink: "←", Dirty: "±", Today: "●", Missing: "×"}
}
