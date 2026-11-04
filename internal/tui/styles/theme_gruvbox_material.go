// Copyright (c) 2026 David Lopes. MIT License; copied from github.com/dnlopes/overseer.

package styles

import "charm.land/lipgloss/v2"

func GruvboxMaterialTheme() Theme {
	return Theme{
		Primary:         lipgloss.Color("#CAA264"),
		Accent:          lipgloss.Color("#A9B665"),
		Warning:         lipgloss.Color("#E78A4E"),
		Danger:          lipgloss.Color("#EA6962"),
		Muted:           lipgloss.Color("#7C6F64"),
		Text:            lipgloss.Color("#D4BE98"),
		Subtext:         lipgloss.Color("#A89984"),
		Border:          lipgloss.Color("#5A524C"),
		BorderFocus:     lipgloss.Color("#CAA264"),
		SelectionBg:     lipgloss.Color("#45403D"),
		TitleText:       lipgloss.Color("#282828"),
		TitleSubtext:    lipgloss.Color("#32302F"),
		HelpBg:          lipgloss.Color("#1B1B1B"),
		HelpBarBg:       lipgloss.Color("#3A3735"),
		HelpKeyBg:       lipgloss.Color("#504945"),
		ModalBg:         lipgloss.Color("#32302F"),
		OverlayBg:       lipgloss.Color("#1B1B1B"),
		StatusRunningFg: lipgloss.Color("#A9B665"),
		StatusRunningBg: lipgloss.Color("#3B4439"),
		StatusWaitingFg: lipgloss.Color("#E78A4E"),
		StatusWaitingBg: lipgloss.Color("#4F422E"),
		StatusIdleFg:    lipgloss.Color("#7C6F64"),
		StatusDeadFg:    lipgloss.Color("#EA6962"),
		StatusDeadBg:    lipgloss.Color("#4C3432"),
		StatusUnknownFg: lipgloss.Color("#A89984"),
	}
}
