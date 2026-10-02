// Copyright (c) 2026 David Lopes. MIT License; copied from github.com/dnlopes/overseer.

package components

import (
	"strings"

	"github.com/osesantos/kb/internal/tui/styles"
)

// HorizontalDivider produces "─" repeated `width` times, styled with s.Divider.Horizontal.
// PURE function.
func HorizontalDivider(s *styles.Styles, width int) string {
	if width <= 0 {
		return ""
	}
	return s.Divider.Horizontal.Render(strings.Repeat("─", width))
}
