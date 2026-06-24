// Copyright 2025 TechDivision GmbH
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package tui provides the interactive terminal UI launcher for valet-sh.
// It is invoked when no arguments are provided to the CLI and allows the
// user to navigate commands with arrow keys and select them interactively.
package tui

import (
	"charm.land/lipgloss/v2"
)

// Colour palette — uses terminal palette indices (0–15) so the TUI adapts
// to the user's terminal theme (Solarized, Dracula, Nord, etc.) rather than
// hardcoding hex values that may clash.
var (
	// colourBlue — terminal bright blue (index 12), matches ansiBlue \033[1;34m.
	colourBlue = lipgloss.Color("12")

	// colourGreen — terminal bright green (index 10), matches ansiGreen \033[0;32m.
	colourGreen = lipgloss.Color("10")

	// colourRed — terminal bright red (index 9), matches ansiRed \033[1;31m.
	colourRed = lipgloss.Color("9")

	// colourDim — terminal bright black / dark grey (index 8).
	colourDim = lipgloss.Color("8")

	// colourText — terminal normal foreground (index 7).
	colourText = lipgloss.Color("7")
)

// styles holds all Lip Gloss styles used by the TUI, initialised once and
// shared across render calls.
var styles = newStyles()

type tuiStyles struct {
	// Header
	Header           lipgloss.Style
	Version          lipgloss.Style
	VimModeIndicator lipgloss.Style
	GhostCommand     lipgloss.Style

	// Horizontal command bar
	CommandSelected   lipgloss.Style
	CommandNormal     lipgloss.Style
	CommandSeparator  lipgloss.Style
	CommandScrollHint lipgloss.Style

	// Inline box (preview + input + docs)
	PreviewBox       lipgloss.Style
	InputGhostPrompt lipgloss.Style
	InputText        lipgloss.Style

	// Description pane (right pane, kept for execScreenView)
	DescTitle lipgloss.Style
	DescBody  lipgloss.Style

	// List items (used by CommandDelegate + exec dimmed list)
	ItemNormal   lipgloss.Style
	ItemSelected lipgloss.Style
	ItemDim      lipgloss.Style

	// Layout
	Divider lipgloss.Style

	// Status bar / help
	HelpKey  lipgloss.Style
	HelpDesc lipgloss.Style
	HelpSep  lipgloss.Style

	// Arg input pane (args.go — kept for future use)
	ArgPaneTitle  lipgloss.Style
	ArgLabel      lipgloss.Style
	ArgInput      lipgloss.Style
	ArgInputFocus lipgloss.Style
	ArgHint       lipgloss.Style
}

func newStyles() tuiStyles {
	return tuiStyles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(colourBlue),

		Version: lipgloss.NewStyle().
			Foreground(colourDim),

		// VimModeIndicator sits left of the version: "  [VIM]  v2.9.19"
		VimModeIndicator: lipgloss.NewStyle().
			Foreground(colourDim),

		// GhostCommand is the currently-hovered command name shown in the header.
		GhostCommand: lipgloss.NewStyle().
			Foreground(colourDim),

		CommandSelected: lipgloss.NewStyle().
			Bold(true).
			Foreground(colourGreen),

		CommandNormal: lipgloss.NewStyle().
			Foreground(colourDim),

		CommandSeparator: lipgloss.NewStyle().
			Foreground(colourDim),

		CommandScrollHint: lipgloss.NewStyle().
			Foreground(colourDim),

		// PreviewBox — rounded border in dim colour, compact padding.
		PreviewBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colourDim).
			Padding(0, 1),

		// InputGhostPrompt — the non-editable "valet.sh service " prefix.
		InputGhostPrompt: lipgloss.NewStyle().
			Foreground(colourDim),

		InputText: lipgloss.NewStyle().
			Foreground(colourText),

		DescTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(colourBlue),

		DescBody: lipgloss.NewStyle().
			Foreground(colourText),

		ItemNormal: lipgloss.NewStyle().
			Foreground(colourText),

		ItemSelected: lipgloss.NewStyle().
			Bold(true).
			Foreground(colourGreen),

		ItemDim: lipgloss.NewStyle().
			Foreground(colourDim),

		Divider: lipgloss.NewStyle().
			Foreground(colourDim),

		HelpKey: lipgloss.NewStyle().
			Foreground(colourGreen),

		HelpDesc: lipgloss.NewStyle().
			Foreground(colourDim),

		HelpSep: lipgloss.NewStyle().
			Foreground(colourDim),

		// Arg pane styles — kept for args.go (future multi-field input).
		ArgPaneTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(colourBlue),

		ArgLabel: lipgloss.NewStyle().
			Foreground(colourDim),

		ArgInput: lipgloss.NewStyle().
			Foreground(colourText),

		ArgInputFocus: lipgloss.NewStyle().
			Foreground(colourGreen),

		ArgHint: lipgloss.NewStyle().
			Foreground(colourDim),
	}
}
