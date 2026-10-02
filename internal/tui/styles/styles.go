// Copyright (c) 2026 David Lopes. MIT License; trimmed from github.com/dnlopes/overseer.

package styles

import (
	"charm.land/lipgloss/v2"
	"image/color"
)

// ListIndentUnit is the column count per nesting level in tree views.
const ListIndentUnit = 2

type BorderStyles struct {
	Focused, Blurred         lipgloss.Style
	CharFocused, CharBlurred lipgloss.Style
	Title                    lipgloss.Style
}

type HelpStyles struct {
	Bar, Key, Description, Separator lipgloss.Style
}

// Styles is every lipgloss style the TUI uses, built once from a Theme.
type Styles struct {
	Border   BorderStyles
	TitleBar struct{ Branding, Subtext, Fill lipgloss.Style }
	Pane     struct{ Container lipgloss.Style }
	ListRow  struct{ Normal, Selected, Aux, AuxSelected lipgloss.Style }
	Group    struct{ Header lipgloss.Style }
	Modal    struct {
		Box          lipgloss.Style
		Overlay      color.Color
		OverlayStyle lipgloss.Style
	}
	Badge    struct{ Key, Label lipgloss.Style }
	Divider  struct{ Horizontal lipgloss.Style }
	Help     HelpStyles
	Empty    struct{ Title, Hint lipgloss.Style }
	TooSmall lipgloss.Style
	Layout   struct{ Box lipgloss.Style }
	Prompt   struct{ Label, Text lipgloss.Style }
	Toast    struct{ Good lipgloss.Style }
	// Status styles the one-line message shown in the help bar's place.
	Status struct{ Info, Error lipgloss.Style }
	Git    struct{ Staged, Unstaged, Added, Removed, Hunk, Meta lipgloss.Style }
	Form   struct{ Title, Label, LabelFocused, Input, Hint, Error lipgloss.Style }
	// Board styles the contribution grid: Shades[0] is an empty day, Shades[4] the busiest.
	Board struct {
		Label, Selected lipgloss.Style
		Shades          [5]lipgloss.Style
	}
	Glyphs Glyphs
}

// NewWithTheme builds Styles from the named theme; unknown names fall back to dark.
func NewWithTheme(themeName string) *Styles {
	t := LoadTheme(themeName)
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	helpKey := lipgloss.NewStyle().Foreground(t.Text).Background(t.HelpBarBg).Bold(true)

	s := &Styles{Glyphs: NewGlyphs()}
	s.Border = BorderStyles{
		Focused:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.BorderFocus),
		Blurred:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Border),
		CharFocused: fg(t.BorderFocus),
		CharBlurred: fg(t.Border),
		Title:       fg(t.Text).Bold(true),
	}
	s.TitleBar.Branding = lipgloss.NewStyle().Background(t.Primary).Foreground(t.TitleText).Bold(true).Padding(0, 1)
	s.TitleBar.Subtext = lipgloss.NewStyle().Background(t.Primary).Foreground(t.TitleSubtext).Padding(0, 1)
	s.TitleBar.Fill = lipgloss.NewStyle().Background(t.Primary)
	s.Pane.Container = lipgloss.NewStyle().Padding(0, 1)
	s.ListRow.Normal = fg(t.Text)
	s.ListRow.Selected = fg(t.Text).Bold(true).Background(t.SelectionBg)
	s.ListRow.Aux = fg(t.Muted)
	s.ListRow.AuxSelected = fg(t.Subtext).Background(t.SelectionBg)
	s.Group.Header = fg(t.Accent).Bold(true)
	onModal := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c).Background(t.ModalBg) }
	s.Form.Title = onModal(t.Primary).Bold(true)
	s.Form.Label = onModal(t.Subtext)
	s.Form.LabelFocused = onModal(t.Accent).Bold(true)
	s.Form.Input = onModal(t.Text)
	s.Form.Hint = onModal(t.Muted)
	s.Form.Error = onModal(t.Warning)
	s.Modal.Box = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.BorderFocus).Background(t.ModalBg).Foreground(t.Text).Padding(1, 3)
	s.Modal.Overlay = t.OverlayBg
	s.Modal.OverlayStyle = lipgloss.NewStyle().Background(t.OverlayBg)
	s.Badge.Key = helpKey
	s.Badge.Label = fg(t.Subtext)
	s.Divider.Horizontal = fg(t.Border)
	s.Help = HelpStyles{
		Bar:         lipgloss.NewStyle().Background(t.HelpBarBg).Padding(0, 1),
		Key:         helpKey,
		Description: fg(t.Subtext).Background(t.HelpBarBg),
		Separator:   fg(t.Muted).Background(t.HelpBarBg),
	}
	s.Empty.Title = fg(t.Subtext).Bold(true)
	s.Empty.Hint = fg(t.Subtext)
	s.TooSmall = fg(t.Warning).Bold(true)
	s.Layout.Box = lipgloss.NewStyle()
	s.Board.Label = fg(t.Subtext)
	s.Board.Selected = fg(t.Warning).Bold(true)
	for i, c := range lipgloss.Blend1D(5, t.SelectionBg, t.Accent) {
		s.Board.Shades[i] = fg(c)
	}
	s.Prompt.Label = fg(t.Accent).Bold(true)
	s.Prompt.Text = fg(t.Text)
	s.Toast.Good = fg(t.Accent).Bold(true)
	s.Status.Info = fg(t.Accent)
	s.Status.Error = fg(t.Warning).Bold(true)
	s.Git.Staged = fg(t.Accent)
	s.Git.Unstaged = fg(t.Danger)
	s.Git.Added = fg(t.Accent)
	s.Git.Removed = fg(t.Danger)
	s.Git.Hunk = fg(t.Primary)
	s.Git.Meta = fg(t.Muted)
	return s
}
