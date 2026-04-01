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
	"github.com/valet-sh/cli/internal/ansible"
)

// screen tracks which state the launcher is in.
type screen int

const (
	screenList   screen = iota // navigating the horizontal command bar
	screenInline               // inline box open (arg input + docs)
	screenExec                 // ansible is running, exec panel shown
)

// stackEntry records a navigation level so we can Esc back.
type stackEntry struct {
	list  list.Model
	title string // breadcrumb label for this level, e.g. "service"
}

// model is the root Bubble Tea model for the valet-sh launcher.
// Elm architecture requires value receivers — do not use pointer receivers.
type model struct {
	// root cobra command — used only to read command metadata.
	root *cobra.Command

	// version string shown in the header.
	version string

	// vimMode enables hjkl navigation when true.
	// Toggled by ctrl+[.
	vimMode bool

	// Navigation stack. stack[0] is always the root menu.
	stack []stackEntry

	// commandList is the bubbles/list model for the current level.
	// Used for keyboard input handling (navigation, filtering) even though
	// the display uses renderHorizontalList() instead of commandList.View().
	commandList list.Model

	// inlineBox is the unified arg-input + docs box shown when a command
	// with arguments (or any command, for previewing) is selected.
	inlineBox *InlineBox

	// activeScreen controls which component receives keystrokes.
	activeScreen screen

	// execModel is the full-width execution panel shown during ansible runs.
	execModel ExecModel

	// width/height of the terminal at last WindowSizeMsg.
	width  int
	height int
}

// Result is returned by Run() after the TUI exits.
type Result struct {
	// Args is the argv slice to dispatch, e.g. ["service", "start", "php83"].
	// Empty means the user cancelled.
	Args []string
}

// Run launches the interactive TUI launcher.
// vimMode starts the launcher with vim-style navigation enabled.
func Run(root *cobra.Command, version string, vimMode bool) (Result, error) {
	m := newModel(root, version, vimMode)
	p := tea.NewProgram(m)

	_, err := p.Run()
	if err != nil {
		return Result{}, err
	}

	// Execution is handled internally by transitioning to screenExec.
	// Result.Args is never populated — we return empty to signal the caller
	// that no further dispatch is needed.
	return Result{}, nil
}

// newModel initialises the launcher model.
func newModel(root *cobra.Command, version string, vimMode bool) model {
	rootList := buildList(root.Commands(), false, 80, 10)
	return model{
		root:        root,
		version:     version,
		vimMode:     vimMode,
		stack:       []stackEntry{{list: rootList, title: "valet.sh"}},
		commandList: rootList,
		width:       80,
		height:      24,
	}
}

// buildList creates a bubbles list.Model from a slice of cobra commands.
// bubbles/list handles keyboard navigation and filter state; the visual
// rendering is delegated to renderHorizontalList().
func buildList(cmds []*cobra.Command, withBack bool, width, height int) list.Model {
	items := itemsFromCommands(cmds, withBack)
	delegate := NewCommandDelegate()
	l := list.New(items, delegate, width, height)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(false)
	l.DisableQuitKeybindings()

	// Style the filter input to match the valet-sh palette.
	l.FilterInput.Prompt = "/ "
	filterStyles := l.FilterInput.Styles()
	filterStyles.Focused.Prompt = styles.InputGhostPrompt.Bold(true).Foreground(colourBlue)
	filterStyles.Focused.Text = styles.InputText
	filterStyles.Blurred.Prompt = styles.InputGhostPrompt
	filterStyles.Blurred.Text = styles.ItemDim
	l.FilterInput.SetStyles(filterStyles)

	return l
}

// --- Elm Architecture ----------------------------------------------------------

// Init satisfies tea.Model.
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

	return m.routeMsg(msg)
}

// handleKey routes keyboard input based on active screen and vim mode.
func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// ctrl+c always quits.
	if key == "ctrl+c" {
		return m, tea.Quit
	}

	// ctrl+[ toggles vim mode from any screen except exec.
	if key == "ctrl+[" && m.activeScreen != screenExec {
		m.vimMode = !m.vimMode
		return m, nil
	}

	switch m.activeScreen {
	case screenList:
		return m.handleListKey(key, msg)

	case screenInline:
		return m.handleInlineKey(key, msg)

	case screenExec:
		var cmd tea.Cmd
		m.execModel, cmd = m.execModel.Update(msg)
		return m, cmd
	}

	return m, nil
}

// handleListKey handles key events on the horizontal command list.
func (m model) handleListKey(key string, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// q quits only when not filtering.
	if key == "q" && m.commandList.FilterState() != list.Filtering {
		return m, tea.Quit
	}

	// Navigation — translate to list cursor movements.
	// Vim mode: h/l/j/k; normal mode: ←/→/↑/↓.
	switch key {
	case "left", "up":
		m.commandList.CursorUp()
		return m, nil
	case "right", "down":
		m.commandList.CursorDown()
		return m, nil
	}

	if m.vimMode {
		switch key {
		case "h", "k":
			m.commandList.CursorUp()
			return m, nil
		case "l", "j":
			m.commandList.CursorDown()
			return m, nil
		}
	}

	switch key {
	case "esc":
		if m.commandList.FilterState() == list.Filtering {
			// Let list handle Esc to clear filter.
		} else {
			return m.popStack()
		}
	case "enter":
		if m.commandList.FilterState() != list.Filtering {
			return m.selectItem()
		}
	}

	// Forward remaining keys to the list (handles filter input).
	var cmd tea.Cmd
	m.commandList, cmd = m.commandList.Update(msg)
	m.stack[len(m.stack)-1].list = m.commandList
	return m, cmd
}

// handleInlineKey handles key events when the inline box is open.
func (m model) handleInlineKey(key string, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		// Close inline box, return to list.
		m.inlineBox = nil
		m.activeScreen = screenList
		return m, nil

	case "enter":
		return m.executeInline()
	}

	// Route to inline box for doc scrolling and text input.
	if m.inlineBox != nil {
		var cmd tea.Cmd
		updated, cmd := m.inlineBox.Update(msg)
		m.inlineBox = &updated
		return m, cmd
	}

	return m, nil
}

// selectItem handles Enter on the command list.
func (m model) selectItem() (tea.Model, tea.Cmd) {
	sel, ok := m.commandList.SelectedItem().(CommandItem)
	if !ok {
		return m, nil
	}

	if sel.IsBack {
		return m.popStack()
	}

	if sel.HasSubCommands() {
		return m.pushStack(sel)
	}

	// Open the inline box — always, so user can review docs and type args.
	path := m.fullCommandPath(sel.Title())
	docs := sel.LongDescription()
	box := NewInlineBox(path, docs, m.inlineBoxWidth())
	m.inlineBox = &box
	m.activeScreen = screenInline
	return m, nil
}

// executeInline executes the command with whatever the user typed in the inline box.
func (m model) executeInline() (tea.Model, tea.Cmd) {
	if m.inlineBox == nil {
		return m, nil
	}

	// Build args: command path + whatever the user typed (free-form).
	args := strings.Fields(m.commandPathFromStack())
	if rawInput := m.inlineBox.Value(); rawInput != "" {
		args = append(args, strings.Fields(rawInput)...)
	}

	opts, err := resolveRunOpts(m.root, args)
	if err != nil {
		return m, tea.Quit
	}

	proc, err := ansible.RunSubprocess(opts)
	if err != nil {
		return m, tea.Quit
	}

	commandStr := strings.Join(args, " ")
	m.execModel = NewExecModel(commandStr, m.version, false, proc, m.width, m.height)
	m.activeScreen = screenExec
	m.inlineBox = nil

	return m, m.execModel.Init()
}

// pushStack navigates into a submenu.
func (m model) pushStack(sel CommandItem) (tea.Model, tea.Cmd) {
	subList := buildList(sel.Cmd.Commands(), true, m.width, 10)
	m.stack = append(m.stack, stackEntry{list: subList, title: sel.Title()})
	m.commandList = subList
	return m, nil
}

// popStack navigates back up one level, or quits at the root.
func (m model) popStack() (tea.Model, tea.Cmd) {
	if len(m.stack) <= 1 {
		return m, tea.Quit
	}
	m.stack = m.stack[:len(m.stack)-1]
	m.commandList = m.stack[len(m.stack)-1].list
	return m, nil
}

// routeMsg forwards non-key messages to the active component.
func (m model) routeMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.activeScreen {
	case screenInline:
		if m.inlineBox != nil {
			updated, c := m.inlineBox.Update(msg)
			m.inlineBox = &updated
			cmd = c
		}
	case screenExec:
		m.execModel, cmd = m.execModel.Update(msg)
	default:
		m.commandList, cmd = m.commandList.Update(msg)
		m.stack[len(m.stack)-1].list = m.commandList
	}
	return m, cmd
}

// resizeAll updates all components after a terminal resize.
func (m model) resizeAll() model {
	m.commandList.SetSize(m.width, 10)
	m.stack[len(m.stack)-1].list = m.commandList

	if m.activeScreen == screenExec {
		m.execModel = m.execModel.SetSize(m.width, m.height)
	}

	return m
}

// --- View -----------------------------------------------------------------------

// View renders the full TUI.
// No alt-screen — renders inline below the cursor like fzf.
func (m model) View() tea.View {
	return tea.NewView(m.render())
}

func (m model) render() string {
	if m.activeScreen == screenExec {
		return m.execModel.View()
	}

	var output strings.Builder

	_, _ = fmt.Fprintln(&output, m.headerView())
	_, _ = fmt.Fprintln(&output, dividerLine(m.width))
	_, _ = fmt.Fprintln(&output, renderHorizontalList(m.commandList, m.width))

	if m.activeScreen == screenInline && m.inlineBox != nil {
		_, _ = fmt.Fprintln(&output, m.inlineBox.View())
		_, _ = fmt.Fprintln(&output, dividerLine(m.width))
		_, _ = fmt.Fprint(&output, renderInlineHelpBar(m.width))
	} else {
		// Show filter input line when actively filtering.
		if m.commandList.FilterState() == list.Filtering {
			_, _ = fmt.Fprintln(&output, "  "+m.commandList.FilterInput.View())
		}
		_, _ = fmt.Fprintln(&output, dividerLine(m.width))
		_, _ = fmt.Fprint(&output, renderHelpBar(m.vimMode, m.width))
	}

	return output.String()
}

func (m model) headerView() string {
	crumbs := make([]string, len(m.stack))
	for i := range m.stack {
		crumbs[i] = m.stack[i].title
	}
	breadcrumb := strings.Join(crumbs, " › ")
	title := styles.Header.Render("▶ " + breadcrumb)

	// Right side of the title — changes based on state:
	// - screenInline: show the live textinput ("valet.sh db █")
	// - screenList:   show dim ghost text of the hovered command ("  db")
	var titleSuffix string
	switch m.activeScreen {
	case screenInline:
		if m.inlineBox != nil {
			titleSuffix = "  " + m.inlineBox.InputView()
		}
	case screenList:
		if sel, ok := m.commandList.SelectedItem().(CommandItem); ok && !sel.IsBack {
			titleSuffix = "  " + styles.GhostCommand.Render(sel.Title())
		}
	}

	// Right side: [VIM] indicator (if active) · version.
	vimIndicator := ""
	if m.vimMode {
		vimIndicator = styles.VimModeIndicator.Render("[VIM]") + "  "
	}
	versionLabel := styles.Version.Render("v" + m.version)
	right := vimIndicator + versionLabel

	// Pad to right edge.
	leftLen := lipgloss.Width(title) + lipgloss.Width(titleSuffix)
	rightLen := lipgloss.Width(right)
	versionPadding := m.width - leftLen - rightLen - 1
	if versionPadding < 1 {
		versionPadding = 1
	}

	return title + titleSuffix + strings.Repeat(" ", versionPadding) + right
}

// --- Layout helpers -------------------------------------------------------------

func dividerLine(width int) string {
	return strings.Repeat("─", width)
}

// inlineBoxWidth returns the width for the inline box — full terminal width.
func (m model) inlineBoxWidth() int {
	return m.width
}

// fullCommandPath returns the full command path for the given leaf name,
// including any submenu breadcrumbs from the navigation stack.
// e.g. if stack is [valet.sh, project] and leaf is "env" → "project env".
func (m model) fullCommandPath(leafName string) string {
	var parts []string
	for i := range m.stack[1:] {
		parts = append(parts, m.stack[i+1].title)
	}
	parts = append(parts, leafName)
	return strings.Join(parts, " ")
}

// commandPathFromStack returns the current command path from the stack
// (excluding root) as a space-separated string, used to build ansible args.
func (m model) commandPathFromStack() string {
	sel, ok := m.commandList.SelectedItem().(CommandItem)
	if !ok {
		return ""
	}
	return m.fullCommandPath(sel.Title())
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
