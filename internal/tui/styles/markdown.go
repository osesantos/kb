// Copyright (c) 2026 David Lopes. MIT License; copied from github.com/dnlopes/overseer.

package styles

import (
	"strings"

	"charm.land/glamour/v2"
)

// markdownStyle is the glamour stylesheet used for all rendered message bodies.
//
// It is pinned rather than auto-detected on purpose: glamour's WithAutoStyle
// probes the terminal background, which makes output depend on where the process
// is running and so breaks golden tests. Overseer's themes are dark-leaning, so
// "dark" is the closest fixed match.
const markdownStyle = "dark"

// Markdown renders message bodies from markdown to ANSI-styled text at a fixed
// width.
//
// It lives in styles/ so glamour's own styling system stays in the one package
// that owns presentation (TUI-03) instead of leaking into feature models, and it
// is instance-scoped rather than a package-level singleton because the wrap width
// is per-panel and changes on resize.
//
// A Markdown with a nil renderer is usable and returns its input unchanged, so a
// glamour construction failure degrades to plain text rather than blanking the
// panel.
type Markdown struct {
	width    int
	renderer *glamour.TermRenderer
}

// NewMarkdown builds a renderer that wraps at width. A width below 1 disables
// rendering, since glamour cannot wrap into no space.
func (s *Styles) NewMarkdown(width int) Markdown {
	if width < 1 {
		return Markdown{}
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(markdownStyle),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return Markdown{width: width}
	}
	return Markdown{width: width, renderer: renderer}
}

// Width reports the wrap width this renderer was built for, so callers can tell
// whether a resize invalidated their cached output.
func (m Markdown) Width() int { return m.width }

// Render converts markdown to styled text. Empty input, a nil renderer, or a
// render failure all yield the input unchanged — a message must never vanish
// because it could not be prettified.
//
// Glamour pads its output with blank lines and a left margin suited to
// full-screen documents; both are trimmed here so a message sits flush in a
// transcript.
func (m Markdown) Render(content string) string {
	if m.renderer == nil || strings.TrimSpace(content) == "" {
		return content
	}

	out, err := m.renderer.Render(content)
	if err != nil {
		return content
	}

	lines := strings.Split(strings.Trim(out, "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "  ")
	}
	rendered := strings.Join(lines, "\n")
	if strings.TrimSpace(rendered) == "" {
		return content
	}
	return rendered
}
