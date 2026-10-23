// SPDX-License-Identifier: AGPL-3.0-or-later

package tui

import "charm.land/lipgloss/v2"

// styles colour the progress view; with colour off every style is plain.
type styles struct {
	ok, fail, dim lipgloss.Style
}

func newStyles(color bool) styles {
	if !color {
		plain := lipgloss.NewStyle()
		return styles{ok: plain, fail: plain, dim: plain}
	}
	return styles{
		ok:   lipgloss.NewStyle().Foreground(lipgloss.Color("#16a34a")),
		fail: lipgloss.NewStyle().Foreground(lipgloss.Color("#dc2626")),
		dim:  lipgloss.NewStyle().Foreground(lipgloss.Color("#6b7280")),
	}
}
