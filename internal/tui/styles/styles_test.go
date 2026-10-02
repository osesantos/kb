package styles

import (
	"testing"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/stretchr/testify/assert"
)

var themes = []string{"dark", "dracula", "github-dark", "tokyo-night", "monokai", "one-dark", "solarized-dark", "nord", "catppuccin-mocha", "porcelain", "deep-sea", "sunset"}

// TestBoardShadesAreEasyToTellApart requires clear lightness steps between neighbouring board shades in every theme;
// the first blend shipped steps of 0.10, which read as almost the same colour.
func TestBoardShadesAreEasyToTellApart(t *testing.T) {
	for _, name := range themes {
		t.Run(name, func(t *testing.T) {
			sh := NewWithTheme(name).Board.Shades
			for i := 1; i < len(sh); i++ {
				a, _ := colorful.MakeColor(sh[i-1].GetForeground())
				b, _ := colorful.MakeColor(sh[i].GetForeground())
				la, _, _ := a.Lab()
				lb, _, _ := b.Lab()
				want := 0.12
				if i == 1 {
					want = 0.15
				}
				dl := lb - la
				if dl < 0 {
					dl = -dl
				}
				assert.GreaterOrEqual(t, dl, want, "shades %d→%d: %s vs %s", i-1, i, a.Hex(), b.Hex())
			}
		})
	}
}
