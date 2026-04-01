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
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
)

// screen tracks which pane has keyboard focus.
type screen int

const (
	screenList screen = iota // navigating the command list
	screenArgs               // filling in argument fields
)

// stackEntry records a navigation level so we can pop back with Esc.
type stackEntry struct {
	list  list.Model
	title string // breadcrumb label for this level
}

// model is the root Bubble Tea model for the valet-sh launcher.
// The Elm-architecture requires value receivers — do not use pointer receivers.
type model struct {
	// cobra root command — used only to read command metadata.
	root *cobra.Command

	// version string shown in the header.
	version string

	// Navigation stack. stack[0] is always the root menu. The current menu
	// is always stack[len(stack)-1].
	stack []stackEntry

	// current is an alias for the top of the stack (kept in sync).
	current list.Model

	// argPane is shown when a command with arguments is selected.
	argPane ArgPane

	// activeScreen controls which pane receives keystrokes.
	activeScreen screen

	// selectedItem holds the command item the user pressed Enter on.
	// Non-nil means the TUI is about to quit with a selection.
	selectedItem *CommandItem

	// width/height of the terminal.
	width  int
	height int
}

// Result is returned by Run() after the TUI exits.
type Result struct {
	// Args is the full argv slice to pass to cobra, e.g. ["service", "start", "php83"].
	// Empty slice means the user cancelled.
	Args []string
}

// Run launches the interactive TUI launcher.
// It takes the cobra root command to introspect subcommands and the version
// string to display in the header.
// It returns the selected command + arguments, or an empty Result if cancelled.
func Run(root *cobra.Command, version string) (Result, error) {
	m := newModel(root, version)
	p := tea.NewProgram(m)

	final, err := p.Run()
	if err != nil {
		return Result{}, err
	}

	fm, ok := final.(model)
	if !ok || fm.selectedItem == nil {
		return Result{}, nil
	}

	// Build argv from selected item + argument pane values.
	// Walk the navigation stack to build the full command path.
	args := commandPath(fm)
	args = append(args, fm.argPane.Values()...)

	return Result{Args: args}, nil
}

// newModel initialises the TUI model from the cobra root command.
func newModel(root *cobra.Command, version string) model {
	rootList := buildList(root.Commands(), false, 80, 20)
	return model{
		root:    root,
		version: version,
		stack:   []stackEntry{{list: rootList, title: "valet.sh"}},
		current: rootList,
		width:   80,
		height:  24,
	}
}

// buildList creates a bubbles list.Model from a slice of cobra commands.
func buildList(cmds []*cobra.Command, withBack bool, width, height int) list.Model {
	items := itemsFromCommands(cmds, withBack)
	delegate := NewCommandDelegate()
	l := list.New(items, delegate, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()
	return l
}

// --- Elm Architecture ----------------------------------------------------------

// Init satisfies tea.Model. No I/O needed at startup.
func (m model) Init() tea.Cmd { return nil }

// Update is the central event handler.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.resizeAll()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// Route all other messages to the active component.
	return m.routeMsg(msg)
}

// handleKey processes keyboard input based on the active screen.
func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Global quit — always works.
	if key == "ctrl+c" || key == "q" {
		return m, tea.Quit
	}

	switch m.activeScreen {
	case screenList:
		switch key {
		case "esc":
			return m.popStack()
		case "enter":
			return m.selectItem()
		}

	case screenArgs:
		switch key {
		case "esc":
			// Cancel arg input, return focus to list.
			m.activeScreen = screenList
			m.argPane = ArgPane{}
			return m, nil
		case "enter":
			if m.argPane.IsReady() {
				return m.executeSelection()
			}
		}
		// Route remaining keys to the arg pane.
		var cmd tea.Cmd
		m.argPane, cmd = m.argPane.Update(msg)
		return m, cmd
	}

	// Forward to the list.
	var cmd tea.Cmd
	m.current, cmd = m.current.Update(msg)
	m.stack[len(m.stack)-1].list = m.current
	return m, cmd
}

// selectItem is called when the user presses Enter on a list item.
func (m model) selectItem() (tea.Model, tea.Cmd) {
	sel, ok := m.current.SelectedItem().(CommandItem)
	if !ok {
		return m, nil
	}

	// "← back" item.
	if sel.IsBack {
		return m.popStack()
	}

	// Command with subcommands → drill into submenu.
	if sel.HasSubCommands() {
		return m.pushStack(sel)
	}

	// Leaf command with required/optional args → show arg pane.
	defs := sel.Args()
	if len(defs) > 0 {
		m.argPane = NewArgPane(defs, m.width)
		m.selectedItem = &sel
		m.activeScreen = screenArgs
		return m, nil
	}

	// Leaf command with no args → execute immediately.
	m.selectedItem = &sel
	return m, tea.Quit
}

// executeSelection is called from screenArgs when the user confirms args.
func (m model) executeSelection() (tea.Model, tea.Cmd) {
	return m, tea.Quit
}

// pushStack navigates into a submenu.
func (m model) pushStack(sel CommandItem) (tea.Model, tea.Cmd) {
	subList := buildList(sel.Cmd.Commands(), true, m.listWidth(), m.listHeight())
	entry := stackEntry{
		list:  subList,
		title: sel.Title(),
	}
	m.stack = append(m.stack, entry)
	m.current = subList
	return m, nil
}

// popStack navigates back up one level, or quits if already at root.
func (m model) popStack() (tea.Model, tea.Cmd) {
	if len(m.stack) <= 1 {
		return m, tea.Quit
	}
	m.stack = m.stack[:len(m.stack)-1]
	m.current = m.stack[len(m.stack)-1].list
	return m, nil
}

// routeMsg forwards non-key messages to the active component.
func (m model) routeMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.activeScreen {
	case screenArgs:
		m.argPane, cmd = m.argPane.Update(msg)
	default:
		m.current, cmd = m.current.Update(msg)
		m.stack[len(m.stack)-1].list = m.current
	}
	return m, cmd
}

// resizeAll updates the sizes of all layout components after a terminal resize.
func (m model) resizeAll() model {
	lw, lh := m.listWidth(), m.listHeight()

	// Resize every list in the stack.
	for i := range m.stack {
		m.stack[i].list.SetSize(lw, lh)
	}
	m.current = m.stack[len(m.stack)-1].list

	// Resize arg pane if visible.
	if m.activeScreen == screenArgs && !m.argPane.IsEmpty() {
		m.argPane.width = m.width
		for j := range m.argPane.inputs {
			m.argPane.inputs[j].SetWidth(m.width/2 - 4)
		}
	}

	return m
}

// --- View -----------------------------------------------------------------------

// View renders the full TUI to a tea.View.
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	return v
}

func (m model) render() string {
	var sb strings.Builder

	// Header
	_, _ = fmt.Fprintln(&sb, m.headerView())
	_, _ = fmt.Fprintln(&sb)

	// Two-pane layout (list left, description right) or arg pane below list.
	if m.activeScreen == screenArgs {
		_, _ = fmt.Fprintln(&sb, m.listAndDescView())
		_, _ = fmt.Fprintln(&sb, dividerLine(m.width))
		_, _ = fmt.Fprintln(&sb, m.argPane.View())
	} else {
		_, _ = fmt.Fprintln(&sb, m.listAndDescView())
	}

	// Help bar.
	_, _ = fmt.Fprintln(&sb)
	_, _ = fmt.Fprintln(&sb, renderHelpBar(m.width))

	return sb.String()
}

func (m model) headerView() string {
	crumbs := make([]string, len(m.stack))
	for i := range m.stack {
		crumbs[i] = m.stack[i].title
	}
	breadcrumb := strings.Join(crumbs, " › ")

	title := styles.Header.Render("▶ " + breadcrumb)
	version := styles.Version.Render("v" + m.version)

	// Pad version to the right edge.
	titleLen := lipgloss.Width(title)
	versionLen := lipgloss.Width(version)
	pad := m.width - titleLen - versionLen - 2
	if pad < 1 {
		pad = 1
	}

	return title + strings.Repeat(" ", pad) + version
}

func (m model) listAndDescView() string {
	lw := m.listWidth()
	rw := m.descWidth()

	leftContent := m.current.View()
	left := styles.LeftPane.Width(lw).Render(leftContent)

	rightContent := m.descriptionView()
	right := styles.RightPane.Width(rw).Render(rightContent)

	divider := styles.Divider.Render(strings.Repeat("│", m.listHeight()))

	return lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
}

func (m model) descriptionView() string {
	sel, ok := m.current.SelectedItem().(CommandItem)
	if !ok || sel.IsBack {
		return styles.DescBody.Render("Select a command to see its description.")
	}

	var sb strings.Builder

	// Command name as title.
	title := sel.Title()
	if sel.HasSubCommands() {
		title += " ›"
	}
	_, _ = fmt.Fprintln(&sb, styles.DescTitle.Render(title))

	// Full description.
	desc := sel.LongDescription()
	wrapped := wordWrap(desc, m.descWidth()-4)
	_, _ = fmt.Fprintln(&sb, styles.DescBody.Render(wrapped))

	return sb.String()
}

// --- Layout helpers -------------------------------------------------------------

const (
	leftPaneFraction = 0.38
	minLeftPaneWidth = 22
	headerHeight     = 3 // header + blank line + divider
	helpBarHeight    = 2 // blank line + help bar
	argPaneMaxHeight = 8
)

func (m model) listWidth() int {
	w := int(float64(m.width) * leftPaneFraction)
	if w < minLeftPaneWidth {
		w = minLeftPaneWidth
	}
	return w
}

func (m model) descWidth() int {
	return m.width - m.listWidth() - 3 // 3 for divider + padding
}

func (m model) listHeight() int {
	h := m.height - headerHeight - helpBarHeight - 2
	if m.activeScreen == screenArgs {
		h -= argPaneMaxHeight
	}
	if h < 4 {
		h = 4
	}
	return h
}

func dividerLine(width int) string {
	return strings.Repeat("─", width)
}

// wordWrap breaks text at word boundaries to fit within maxWidth columns.
func wordWrap(text string, maxWidth int) string {
	if maxWidth <= 0 {
		return text
	}
	words := strings.Fields(text)
	var lines []string
	var current strings.Builder

	for _, word := range words {
		switch {
		case current.Len() == 0:
			current.WriteString(word)
		case current.Len()+1+len(word) <= maxWidth:
			current.WriteByte(' ')
			current.WriteString(word)
		default:
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(word)
		}
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return strings.Join(lines, "\n")
}

// commandPath walks the navigation stack to reconstruct the full command path
// for the selected item (e.g. ["project", "env"] or ["service"]).
func commandPath(m model) []string {
	if m.selectedItem == nil {
		return nil
	}

	// Walk the stack breadcrumbs (skip root entry at index 0 which is "valet.sh").
	var path []string
	for i := range m.stack[1:] {
		path = append(path, m.stack[i+1].title)
	}
	path = append(path, m.selectedItem.Title())
	return path
}
