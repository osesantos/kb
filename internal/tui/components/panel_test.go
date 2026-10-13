package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"

	"github.com/osesantos/kb/internal/tui/styles"
)

func TestPanelWithTitleFastPathMatchesLipgloss(t *testing.T) {
	s := styles.NewWithTheme("dark")
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render
	for _, content := range []string{"", "one", "a\n\nb", red("styled") + " 🙂 x", strings.Repeat("l\n", 30), strings.Repeat("w", 18)} {
		for _, focused := range []bool{true, false} {
			fast := PanelWithTitle(s, content, "T", focused, 22, 8).Content
			_, ok := fitRows(s, content, 20, 6)
			assert.True(t, ok, "fits: %q", content)
			slow := slowPanel(s, content, "T", focused, 22, 8)
			assert.Equal(t, ansi.Strip(slow), ansi.Strip(fast), "content %q", content)
		}
	}
}

// slowPanel is the lipgloss-only layout the fast path replaces.
func slowPanel(s *styles.Styles, content, title string, focused bool, width, height int) string {
	border := panelBorder(s, focused)
	bw, bh := border.GetFrameSize()
	padded := s.Pane.Container.PaddingBottom(titledPanelPaddingBottom).Width(width - bw).Height(height - bh).MaxHeight(height - bh).Render(content)
	chars := lipgloss.RoundedBorder()
	top := chars.TopLeft + chars.Top + " " + title + " " + strings.Repeat(chars.Top, width-2-1-lipgloss.Width(title)-2) + chars.TopRight
	return lipgloss.JoinVertical(lipgloss.Left, top, border.BorderTop(false).Width(width).Height(height-1).Render(padded))
}
