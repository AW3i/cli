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

// Colour palette — mirrors the ANSI codes used by the Python Ansible callback
// plugin and internal/commands/help.go so the TUI feels visually consistent.
var (
	colourBlue  = lipgloss.Color("#1E90FF") // matches ansiBlue  \033[1;34m
	colourGreen = lipgloss.Color("#00CC00") // matches ansiGreen \033[0;32m
	colourRed   = lipgloss.Color("#FF3333") // matches ansiRed   \033[1;31m
	colourDim   = lipgloss.Color("#666666")
	colourText  = lipgloss.Color("#DDDDDD")
)

// styles holds all Lip Gloss styles used by the TUI.
// They are initialised once and shared across render calls.
var styles = newStyles()

type tuiStyles struct {
	// Outer layout
	App       lipgloss.Style
	LeftPane  lipgloss.Style
	RightPane lipgloss.Style
	Divider   lipgloss.Style

	// Header
	Header     lipgloss.Style
	Breadcrumb lipgloss.Style
	Version    lipgloss.Style

	// List items
	ItemNormal   lipgloss.Style
	ItemSelected lipgloss.Style
	ItemDim      lipgloss.Style

	// Description pane
	DescTitle lipgloss.Style
	DescBody  lipgloss.Style

	// Arg input pane
	ArgPaneTitle  lipgloss.Style
	ArgLabel      lipgloss.Style
	ArgInput      lipgloss.Style
	ArgInputFocus lipgloss.Style
	ArgHint       lipgloss.Style

	// Status bar / help
	HelpKey  lipgloss.Style
	HelpDesc lipgloss.Style
	HelpSep  lipgloss.Style
}

func newStyles() tuiStyles {
	return tuiStyles{
		App: lipgloss.NewStyle().Padding(0),

		LeftPane: lipgloss.NewStyle().
			PaddingRight(1),

		RightPane: lipgloss.NewStyle().
			PaddingLeft(2),

		Divider: lipgloss.NewStyle().
			Foreground(colourDim),

		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(colourBlue).
			PaddingBottom(1),

		Breadcrumb: lipgloss.NewStyle().
			Foreground(colourDim),

		Version: lipgloss.NewStyle().
			Foreground(colourDim).
			Italic(true),

		ItemNormal: lipgloss.NewStyle().
			Foreground(colourText),

		ItemSelected: lipgloss.NewStyle().
			Bold(true).
			Foreground(colourGreen),

		ItemDim: lipgloss.NewStyle().
			Foreground(colourDim),

		DescTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(colourBlue).
			PaddingBottom(1),

		DescBody: lipgloss.NewStyle().
			Foreground(colourText),

		ArgPaneTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(colourBlue).
			PaddingBottom(1),

		ArgLabel: lipgloss.NewStyle().
			Foreground(colourDim),

		ArgInput: lipgloss.NewStyle().
			Foreground(colourText),

		ArgInputFocus: lipgloss.NewStyle().
			Foreground(colourGreen),

		ArgHint: lipgloss.NewStyle().
			Foreground(colourDim).
			Italic(true),

		HelpKey: lipgloss.NewStyle().
			Foreground(colourGreen),

		HelpDesc: lipgloss.NewStyle().
			Foreground(colourDim),

		HelpSep: lipgloss.NewStyle().
			Foreground(colourDim),
	}
}
