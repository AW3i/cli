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

// Colour palette — uses terminal palette indices (0–15) to adapt to the user's
// terminal theme (Solarized, Dracula, Nord, etc.) rather than hardcoding hex values.
var (
	colourBlue  = lipgloss.Color("12") // bright blue (matches \033[1;34m)
	colourGreen = lipgloss.Color("10") // bright green (matches \033[0;32m)
	colourRed   = lipgloss.Color("9")  // bright red (matches \033[1;31m)
	colourDim   = lipgloss.Color("8")  // dark grey
	colourText  = lipgloss.Color("7")  // normal foreground
)

// styles holds all Lip Gloss styles used by the TUI, initialised once and
// shared across render calls.
var styles = newStyles()

type tuiStyles struct {
	Header           lipgloss.Style
	Version          lipgloss.Style
	VimModeIndicator lipgloss.Style
	GhostCommand     lipgloss.Style

	CommandSelected   lipgloss.Style
	CommandNormal     lipgloss.Style
	CommandSeparator  lipgloss.Style
	CommandScrollHint lipgloss.Style

	PreviewBox       lipgloss.Style
	InputGhostPrompt lipgloss.Style
	InputText        lipgloss.Style

	ItemSelected lipgloss.Style
	ItemDim      lipgloss.Style

	Divider lipgloss.Style

	HelpKey  lipgloss.Style
	HelpDesc lipgloss.Style
	HelpSep  lipgloss.Style
}

func newStyles() tuiStyles {
	return tuiStyles{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(colourBlue),

		Version: lipgloss.NewStyle().
			Foreground(colourDim),

		VimModeIndicator: lipgloss.NewStyle().
			Foreground(colourDim),

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

		PreviewBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colourDim).
			Padding(0, 1),

		InputGhostPrompt: lipgloss.NewStyle().
			Foreground(colourDim),

		InputText: lipgloss.NewStyle().
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
	}
}
