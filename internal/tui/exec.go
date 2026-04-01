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
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	// logPath is where the Ansible callback plugin writes all task output.
	logPath = "/usr/local/valet-sh/valet-sh/log/debug.log"

	// logPollInterval is how often we check for new log lines while running.
	logPollInterval = 100 * time.Millisecond

	// viewportMinHeight ensures the log area is always usable.
	viewportMinHeight = 5

	// execHeaderHeight: command line only (no progress bar placeholder).
	execHeaderHeight = 1

	// execFooterHeight: divider + status/hint line.
	execFooterHeight = 2

	// execFooterHeightPrompt: divider + failure line + prompt line.
	// Used when awaiting the "View full log?" response so the viewport
	// does not jump when the prompt appears.
	execFooterHeightPrompt = 3

	// logViewMaxLines is the maximum number of lines loaded from the log
	// file when the viewer is opened after a failure. Keeps rendering fast
	// even for very large log files accumulated over many runs.
	logViewMaxLines = 10_000

	// logViewHeaderHeight: file path + divider.
	logViewHeaderHeight = 2

	// logViewFooterHeight: hint line.
	logViewFooterHeight = 1
)

// execTickMsg fires periodically to poll the log file for new content.
type execTickMsg time.Time

// logLineMsg carries a new line read from the log file.
type logLineMsg string

// execDoneMsg is sent when the ansible-playbook process exits.
type execDoneMsg struct{ err error }

// logViewReadyMsg is sent when tailFile() has finished reading the log.
// It is delivered via tea.Cmd so the file read never blocks the render loop.
type logViewReadyMsg struct{ lines []string }

// ExecModel is a Bubble Tea model that shows a scrollable live log panel
// while an ansible-playbook subprocess is running. On failure it offers
// to open a full-screen log viewer.
//
// It can be embedded inside the launcher (withSidebar=true) or used
// standalone as a full-screen panel (withSidebar=false).
type ExecModel struct {
	// command is the display string shown in the header, e.g. "service start php83".
	command string

	// version string shown in the header.
	version string

	// withSidebar is true when embedded inside the TUI launcher.
	withSidebar bool

	// proc is the running ansible-playbook subprocess.
	proc *exec.Cmd

	// logFile is the open debug.log handle seeked to EOF before the run.
	logFile *os.File

	// viewport is the rolling live-log panel shown during execution.
	viewport viewport.Model

	// logViewer is the full-screen viewport shown when the user chooses
	// to view the full log after a failure.
	logViewer viewport.Model

	// done is true once the subprocess has exited.
	done bool

	// err is non-nil if ansible exited with a non-zero status.
	err error

	// awaitingLogPrompt is true when we have shown the "View full log? [Y/n]"
	// prompt and are waiting for the user's answer.
	awaitingLogPrompt bool

	// logViewOpen is true once the user has said Y and the log viewer is active.
	logViewOpen bool

	// width/height of the available area.
	width  int
	height int
}

// NewExecModel creates a new ExecModel ready to run.
// proc must already be started via ansible.RunSubprocess().
// width/height are the dimensions of the panel area.
func NewExecModel(command, version string, withSidebar bool, proc *exec.Cmd, width, height int) ExecModel {
	viewport := viewport.New(viewport.WithWidth(width), viewport.WithHeight(execViewportHeight(height, false)))
	viewport.SetContent("")

	// Open the log file and seek to the current end so we only tail lines
	// written during this run, not historical content.
	logFile, _ := os.Open(logPath)
	if logFile != nil {
		_, _ = logFile.Seek(0, io.SeekEnd)
	}

	return ExecModel{
		command:     command,
		version:     version,
		withSidebar: withSidebar,
		proc:        proc,
		logFile:     logFile,
		viewport:    viewport,
		width:       width,
		height:      height,
	}
}

// SetSize updates the panel dimensions (called on terminal resize).
func (e ExecModel) SetSize(width, height int) ExecModel {
	e.width = width
	e.height = height
	e.viewport.SetWidth(width)
	e.viewport.SetHeight(execViewportHeight(height, e.awaitingLogPrompt))
	if e.logViewOpen {
		e.logViewer.SetWidth(width)
		e.logViewer.SetHeight(logViewerViewportHeight(height))
	}
	return e
}

// Init starts the log poller and the process waiter.
func (e ExecModel) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		waitForProcess(e.proc),
	)
}

// Update handles log lines, tick events, process exit, and key presses.
func (e ExecModel) Update(msg tea.Msg) (ExecModel, tea.Cmd) {
	switch msg := msg.(type) {
	case execTickMsg:
		lines := e.readNewLogLines()
		if len(lines) > 0 {
			e.appendLines(lines)
		}
		if !e.done {
			return e, tickCmd()
		}
		return e, nil

	case logLineMsg:
		e.appendLine(string(msg))
		return e, nil

	case execDoneMsg:
		// Final log drain before marking done.
		lines := e.readNewLogLines()
		if len(lines) > 0 {
			e.appendLines(lines)
		}
		e.done = true
		e.err = msg.err
		if e.logFile != nil {
			_ = e.logFile.Close()
			e.logFile = nil
		}
		// On failure: show the prompt. Resize viewport to make room.
		if e.err != nil {
			e.awaitingLogPrompt = true
			e.viewport.SetHeight(execViewportHeight(e.height, true))
		}
		return e, nil

	case logViewReadyMsg:
		// The log file has been read — open the full-screen viewer.
		e.logViewer = viewport.New(
			viewport.WithWidth(e.width),
			viewport.WithHeight(logViewerViewportHeight(e.height)),
		)
		e.logViewer.SetContent(strings.Join(msg.lines, "\n"))
		e.logViewer.GotoBottom()
		e.logViewOpen = true
		return e, nil

	case tea.KeyPressMsg:
		return e.handleKey(msg)

	case tea.WindowSizeMsg:
		e = e.SetSize(msg.Width, msg.Height)
		return e, nil
	}

	// Route remaining messages to the active viewport.
	var cmd tea.Cmd
	if e.logViewOpen {
		e.logViewer, cmd = e.logViewer.Update(msg)
	} else {
		e.viewport, cmd = e.viewport.Update(msg)
	}
	return e, cmd
}

// handleKey processes keyboard input for all exec sub-states.
func (e ExecModel) handleKey(msg tea.KeyPressMsg) (ExecModel, tea.Cmd) {
	key := msg.String()

	// Log viewer is open — scroll or quit.
	if e.logViewOpen {
		switch key {
		case "q", "esc", "ctrl+c":
			return e, tea.Quit
		}
		var cmd tea.Cmd
		e.logViewer, cmd = e.logViewer.Update(msg)
		return e, cmd
	}

	// Awaiting Y/n prompt after failure.
	if e.awaitingLogPrompt {
		switch key {
		case "y", "enter":
			e.awaitingLogPrompt = false
			return e, loadLogCmd()
		case "n", "esc", "q", "ctrl+c":
			return e, tea.Quit
		}
		// Silently ignore all other keys while prompting.
		return e, nil
	}

	// Still running — scroll viewport or handle ctrl+c.
	if !e.done {
		if key == "ctrl+c" {
			return e, tea.Quit
		}
		var cmd tea.Cmd
		e.viewport, cmd = e.viewport.Update(msg)
		return e, cmd
	}

	// Done (success) — any key exits.
	return e, tea.Quit
}

// View renders the execution panel or the log viewer.
func (e ExecModel) View() string {
	if e.logViewOpen {
		return e.logViewerView()
	}
	return e.execView()
}

// execView renders the live execution panel.
func (e ExecModel) execView() string {
	var output strings.Builder

	// Header: command being run + version.
	cmdLabel := styles.Header.Render("▶ valet.sh " + e.command)
	versionLabel := styles.Version.Render("v" + e.version)
	versionPadding := e.width - lipgloss.Width(cmdLabel) - lipgloss.Width(versionLabel) - 2
	if versionPadding < 1 {
		versionPadding = 1
	}
	_, _ = fmt.Fprintln(&output, cmdLabel+strings.Repeat(" ", versionPadding)+versionLabel)

	// Live log viewport — no progress bar placeholder, just the output.
	_, _ = fmt.Fprintln(&output, e.viewport.View())

	// Footer.
	_, _ = fmt.Fprintln(&output, styles.Divider.Render(strings.Repeat("─", e.width)))
	_, _ = fmt.Fprint(&output, e.statusLines())

	return output.String()
}

// logViewerView renders the full-screen log file viewer.
func (e ExecModel) logViewerView() string {
	var output strings.Builder

	// Header: file path + line count hint.
	header := styles.Header.Render("▶ " + logPath)
	hint := styles.Version.Render(fmt.Sprintf("(last %d lines)", logViewMaxLines))
	versionPadding := e.width - lipgloss.Width(header) - lipgloss.Width(hint) - 2
	if versionPadding < 1 {
		versionPadding = 1
	}
	_, _ = fmt.Fprintln(&output, header+strings.Repeat(" ", versionPadding)+hint)
	_, _ = fmt.Fprintln(&output, styles.Divider.Render(strings.Repeat("─", e.width)))

	// Scrollable viewport.
	_, _ = fmt.Fprintln(&output, e.logViewer.View())

	// Footer hint.
	_, _ = fmt.Fprintln(&output, styles.Divider.Render(strings.Repeat("─", e.width)))
	_, _ = fmt.Fprint(&output,
		styles.HelpKey.Render("↑/↓")+
			styles.HelpDesc.Render(" scroll   ")+
			styles.HelpKey.Render("q/esc")+
			styles.HelpDesc.Render(" exit"),
	)

	return output.String()
}

// statusLines returns the footer content — one or two lines depending on state.
func (e ExecModel) statusLines() string {
	if !e.done {
		return styles.HelpDesc.Render("⠋ running...   ↑/↓ scroll")
	}

	if e.err != nil {
		failLine := lipgloss.NewStyle().Foreground(colourRed).Bold(true).Render(
			"✘ failed — see " + logPath,
		)
		if e.awaitingLogPrompt {
			promptLine := styles.HelpDesc.Render("View full log? ") +
				styles.HelpKey.Render("[Y]") +
				styles.HelpDesc.Render("/") +
				styles.ItemDim.Render("n")
			return failLine + "\n" + promptLine
		}
		return failLine + "\n" + styles.HelpDesc.Render("(press any key to exit)")
	}

	return styles.ItemSelected.Render("✔ done") +
		"   " + styles.HelpDesc.Render("(press any key to exit)")
}

// IsDone returns true once the subprocess has exited.
func (e ExecModel) IsDone() bool { return e.done }

// Err returns the subprocess exit error, if any.
func (e ExecModel) Err() error { return e.err }

// appendLine appends a single line to the live viewport.
func (e *ExecModel) appendLine(line string) {
	current := e.viewport.GetContent()
	if current == "" {
		e.viewport.SetContent(line)
	} else {
		e.viewport.SetContent(current + "\n" + line)
	}
	e.viewport.GotoBottom()
}

// appendLines appends multiple lines to the live viewport in one call.
func (e *ExecModel) appendLines(lines []string) {
	joined := strings.Join(lines, "\n")
	current := e.viewport.GetContent()
	if current == "" {
		e.viewport.SetContent(joined)
	} else {
		e.viewport.SetContent(current + "\n" + joined)
	}
	e.viewport.GotoBottom()
}

// readNewLogLines reads any lines appended to the log file since the last read.
func (e *ExecModel) readNewLogLines() []string {
	if e.logFile == nil {
		return nil
	}
	var lines []string
	scanner := bufio.NewScanner(e.logFile)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

// tickCmd returns a tea.Cmd that fires after logPollInterval.
func tickCmd() tea.Cmd {
	return tea.Tick(logPollInterval, func(t time.Time) tea.Msg {
		return execTickMsg(t)
	})
}

// waitForProcess waits for the subprocess to exit and sends execDoneMsg.
func waitForProcess(cmd *exec.Cmd) tea.Cmd {
	return func() tea.Msg {
		if cmd == nil {
			return execDoneMsg{}
		}
		return execDoneMsg{err: cmd.Wait()}
	}
}

// loadLogCmd reads up to logViewMaxLines from the end of the log file
// and delivers a logViewReadyMsg. Runs inside a tea.Cmd goroutine so the
// file read never blocks the render loop.
func loadLogCmd() tea.Cmd {
	return func() tea.Msg {
		lines, err := tailFile(logPath, logViewMaxLines)
		if err != nil {
			return logViewReadyMsg{lines: []string{
				"(could not read log: " + err.Error() + ")",
			}}
		}
		return logViewReadyMsg{lines: lines}
	}
}

// tailFile reads up to maxLines from the end of the file at path using a
// ring buffer — single O(n) pass, O(maxLines) memory, no backward seeking.
func tailFile(path string, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	ring := make([]string, maxLines)
	pos, count := 0, 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		ring[pos%maxLines] = scanner.Text()
		pos++
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if count <= maxLines {
		result := make([]string, count)
		copy(result, ring[:count])
		return result, nil
	}

	// Reassemble in chronological order (oldest → newest).
	start := pos % maxLines
	result := make([]string, maxLines)
	copy(result, ring[start:])
	copy(result[maxLines-start:], ring[:start])
	return result, nil
}

// execViewportHeight calculates the live-log viewport height.
// When awaitingPrompt is true an extra line is reserved for the prompt.
func execViewportHeight(totalHeight int, awaitingPrompt bool) int {
	footer := execFooterHeight
	if awaitingPrompt {
		footer = execFooterHeightPrompt
	}
	h := totalHeight - execHeaderHeight - footer
	if h < viewportMinHeight {
		h = viewportMinHeight
	}
	return h
}

// logViewerViewportHeight calculates the log viewer viewport height.
func logViewerViewportHeight(totalHeight int) int {
	h := totalHeight - logViewHeaderHeight - logViewFooterHeight
	if h < viewportMinHeight {
		h = viewportMinHeight
	}
	return h
}

// IsTTY reports whether stdout is connected to a terminal.
func IsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
