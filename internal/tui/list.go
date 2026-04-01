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

package tui

import (
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
)

// CommandItem represents a single cobra command in the TUI list.
// It implements list.DefaultItem so it works with the default delegate.
type CommandItem struct {
	// Cmd is the underlying cobra command.
	Cmd *cobra.Command

	// IsBack is true for the synthetic "← back" item shown in submenus.
	IsBack bool
}

func (i CommandItem) Title() string {
	if i.IsBack {
		return "← back"
	}
	// Use only the first word of Use (the command name without arg placeholders).
	return strings.SplitN(i.Cmd.Use, " ", 2)[0]
}

func (i CommandItem) Description() string {
	if i.IsBack {
		return "Return to the previous menu"
	}
	return i.Cmd.Short
}

func (i CommandItem) FilterValue() string { return i.Title() }

// LongDescription returns the full description to show in the right pane.
func (i CommandItem) LongDescription() string {
	if i.IsBack {
		return "Return to the previous menu"
	}
	if i.Cmd.Long != "" {
		return strings.TrimSpace(i.Cmd.Long)
	}
	return i.Cmd.Short
}

// HasSubCommands returns true when this item should drill into a submenu
// rather than execute directly.
func (i CommandItem) HasSubCommands() bool {
	if i.IsBack {
		return false
	}
	return i.Cmd.HasAvailableSubCommands()
}

// Args returns the cobra flag definitions for commands that need arguments,
// presented as a slice of ArgDef for the argument input pane.
func (i CommandItem) Args() []ArgDef {
	if i.IsBack || i.Cmd == nil {
		return nil
	}
	return argsFromUse(i.Cmd.Use)
}

// ArgDef describes a single argument slot for a command.
type ArgDef struct {
	Name     string
	Required bool
}

// argsFromUse parses cobra Use strings like "service <action> [service-name]"
// into a slice of ArgDef. Required args are wrapped in <>, optional in [].
func argsFromUse(use string) []ArgDef {
	parts := strings.Fields(use)
	if len(parts) <= 1 {
		return nil // no args in Use string
	}

	var defs []ArgDef
	for _, part := range parts[1:] {
		// Skip variadic markers like [args...]
		if strings.HasSuffix(part, "...]") || strings.HasSuffix(part, "...>") {
			continue
		}
		if strings.HasPrefix(part, "<") && strings.HasSuffix(part, ">") {
			defs = append(defs, ArgDef{
				Name:     strings.Trim(part, "<>"),
				Required: true,
			})
		} else if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
			defs = append(defs, ArgDef{
				Name:     strings.Trim(part, "[]"),
				Required: false,
			})
		}
	}
	return defs
}

// itemsFromCommands converts a slice of cobra commands into list.Item values.
// When withBack is true, a synthetic "← back" item is prepended.
func itemsFromCommands(cmds []*cobra.Command, withBack bool) []list.Item {
	items := make([]list.Item, 0, len(cmds)+1)
	if withBack {
		items = append(items, CommandItem{IsBack: true})
	}
	for _, cmd := range cmds {
		if cmd.IsAvailableCommand() {
			items = append(items, CommandItem{Cmd: cmd})
		}
	}
	return items
}

// CommandDelegate is a custom list delegate that renders command items
// using the valet-sh colour palette.
type CommandDelegate struct {
	list.DefaultDelegate
}

// NewCommandDelegate creates a delegate styled to match valet-sh's visual language.
func NewCommandDelegate() CommandDelegate {
	d := list.NewDefaultDelegate()

	// Single line — description shown in the right pane instead.
	d.ShowDescription = false
	d.SetHeight(1)

	// Selected item: green + bold, matching the Ansible callback play_start colour.
	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colourGreen).
		PaddingLeft(2)

	// Unselected item: normal text.
	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(colourText).
		PaddingLeft(2)

	// Dim item (filtered out).
	d.Styles.DimmedTitle = lipgloss.NewStyle().
		Foreground(colourDim).
		PaddingLeft(2)

	// Remove the border on selected item (we use colour only).
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.BorderLeft(false)

	return CommandDelegate{DefaultDelegate: d}
}

// renderHelpBar renders the keybinding hint line at the bottom of the TUI.
func renderHelpBar(width int) string {
	bindings := []struct{ key, desc string }{
		{"↑/↓", "navigate"},
		{"↵", "select"},
		{"esc", "back"},
		{"q", "quit"},
	}

	parts := make([]string, 0, len(bindings)*2)
	for i, b := range bindings {
		parts = append(parts,
			styles.HelpKey.Render(b.key),
			styles.HelpDesc.Render(b.desc),
		)
		if i < len(bindings)-1 {
			parts = append(parts, styles.HelpSep.Render(" · "))
		}
	}

	bar := strings.Join(parts, " ")
	// width is reserved for future centred rendering
	_ = width
	return bar
}
