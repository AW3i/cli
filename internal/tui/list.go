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

// LongDescription returns the full description to show in the inline box.
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

// Args returns the cobra flag definitions for commands that need arguments.
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
		return nil
	}

	var defs []ArgDef
	for _, part := range parts[1:] {
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

// prefixFilter filters items by prefix match (case-insensitive).
// Returns a rank for each item that starts with the search term.
func prefixFilter(term string, targets []string) []list.Rank {
	lower := strings.ToLower(term)
	var ranks []list.Rank
	for i, t := range targets {
		if strings.HasPrefix(strings.ToLower(t), lower) {
			ranks = append(ranks, list.Rank{Index: i})
		}
	}
	return ranks
}

// CommandDelegate is a custom list delegate — kept for compatibility with
// bubbles/list internals. The horizontal command bar uses renderHorizontalList()
// for display instead of commandList.View().
type CommandDelegate struct {
	list.DefaultDelegate
}

// NewCommandDelegate creates a minimal delegate — spacing 0, single line.
// Styles are not used for display (renderHorizontalList handles rendering)
// but must be set so bubbles/list does not panic on internal calls.
func NewCommandDelegate() CommandDelegate {
	d := list.NewDefaultDelegate()
	d.ShowDescription = false
	d.SetHeight(1)
	d.SetSpacing(0)

	d.Styles.SelectedTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colourGreen).
		PaddingLeft(1)

	d.Styles.NormalTitle = lipgloss.NewStyle().
		Foreground(colourText).
		PaddingLeft(1)

	d.Styles.DimmedTitle = lipgloss.NewStyle().
		Foreground(colourDim).
		PaddingLeft(1)

	d.Styles.SelectedTitle = d.Styles.SelectedTitle.BorderLeft(false)

	return CommandDelegate{DefaultDelegate: d}
}

// renderHorizontalList renders the visible items from commandList as a
// single scrollable horizontal row:
//
//	← install · init · ▶ init-instance · service · link →
//
// The selected item is highlighted in green with a ▶ prefix.
// Scroll arrows (← ▷) appear at the edges when items are off-screen.
// The rendered string is guaranteed to fit within maxWidth characters.
func renderHorizontalList(commandList list.Model, maxWidth int) string {
	items := commandList.VisibleItems()
	if len(items) == 0 {
		return ""
	}

	selectedIdx := commandList.Index()
	sep := styles.CommandSeparator.Render(" · ")

	// Build all item labels.
	labels := make([]string, len(items))
	for i, item := range items {
		cmd, ok := item.(CommandItem)
		if !ok {
			continue
		}
		if i == selectedIdx {
			labels[i] = styles.CommandSelected.Render("▶ " + cmd.Title())
		} else {
			labels[i] = styles.CommandNormal.Render(cmd.Title())
		}
	}

	// Build a left-anchored sliding window that fits in maxWidth.
	// Reserve space for the scroll indicators (2 chars each side).
	// Subtract 1 for ambiguous-width characters (e.g., ▶ in Nerd Fonts renders as 2 cols).
	const scrollIndicatorWidth = 2
	available := maxWidth - scrollIndicatorWidth*2 - 1

	// Fill the window from the left (index 0) as far right as width allows.
	start := 0
	end := 0
	current := 0

	for end < len(labels) {
		itemWidth := lipgloss.Width(labels[end])
		if end > 0 {
			itemWidth += lipgloss.Width(sep)
		}
		if current+itemWidth > available {
			break
		}
		current += itemWidth
		end++
	}

	// If the selected item is beyond the current window, slide the window
	// rightward until it is visible. The left edge only advances when forced.
	for selectedIdx >= end {
		// Remove the leftmost item from the window.
		leftWidth := lipgloss.Width(labels[start])
		if start > 0 {
			leftWidth += lipgloss.Width(sep)
		}
		current -= leftWidth
		start++

		// Add the next item on the right if there is one.
		if end < len(labels) {
			rightWidth := lipgloss.Width(labels[end])
			if end > start {
				rightWidth += lipgloss.Width(sep)
			}
			current += rightWidth
			end++
		}
	}

	// Assemble the visible segment.
	var parts []string
	for i := start; i < end; i++ {
		parts = append(parts, labels[i])
	}
	row := strings.Join(parts, sep)

	// Add scroll indicators.
	leftHint := "  "
	rightHint := "  "
	if start > 0 {
		leftHint = styles.CommandScrollHint.Render("← ")
	}
	if end < len(labels) {
		rightHint = styles.CommandScrollHint.Render(" →")
	}

	return leftHint + row + rightHint
}

// renderHelpBar renders the keybinding hint line at the bottom of the TUI.
func renderHelpBar(vimMode bool, width int) string {
	var bindings []struct{ key, desc string }

	if vimMode {
		bindings = []struct{ key, desc string }{
			{"h/l", "navigate"},
			{"/", "search"},
			{"?", "help"},
			{"↵", "select"},
			{"esc", "back"},
			{"q", "quit"},
			{"ctrl+[", "normal mode"},
		}
	} else {
		bindings = []struct{ key, desc string }{
			{"←/→", "navigate"},
			{"?", "help"},
			{"↵", "select"},
			{"esc", "back"},
			{"q", "quit"},
			{"ctrl+[", "vim mode"},
		}
	}

	parts := make([]string, 0, len(bindings)*2)
	for i, b := range bindings {
		if b.desc == "" {
			parts = append(parts, styles.HelpKey.Render(b.key))
		} else {
			parts = append(parts,
				styles.HelpKey.Render(b.key),
				styles.HelpDesc.Render(" "+b.desc),
			)
		}
		if i < len(bindings)-1 {
			parts = append(parts, styles.HelpSep.Render(" · "))
		}
	}

	bar := strings.Join(parts, " ")
	_ = width // reserved for future centred rendering
	return bar
}

// renderInlineHelpBar renders the help line shown while the inline box is open.
func renderInlineHelpBar(_ int) string {
	bindings := []struct{ key, desc string }{
		{"?", "help"},
		{"↵", "run"},
		{"ctrl+d/u", "scroll docs"},
		{"esc", "back"},
	}

	parts := make([]string, 0, len(bindings)*2)
	for i, b := range bindings {
		parts = append(parts,
			styles.HelpKey.Render(b.key),
			styles.HelpDesc.Render(" "+b.desc),
		)
		if i < len(bindings)-1 {
			parts = append(parts, styles.HelpSep.Render(" · "))
		}
	}

	return strings.Join(parts, " ")
}
