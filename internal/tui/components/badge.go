// Copyright (c) 2026 David Lopes. MIT License; copied from github.com/dnlopes/overseer.

package components

import "github.com/osesantos/kb/internal/tui/styles"

func KeyBadge(s *styles.Styles, key, label string) string {
	return s.Badge.Key.Render(key) + " " + s.Badge.Label.Render(label)
}
