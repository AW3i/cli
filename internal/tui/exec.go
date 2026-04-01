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

	// execHeaderHeight: command line + divider + progress placeholder + divider
	execHeaderHeight = 4

	// execFooterHeight: status line + hint line
	execFooterHeight = 2
)

// execTickMsg fires periodically to poll the log file for new content.
type execTickMsg time.Time

// logLineMsg carries a new line read from the log file.
type logLineMsg string

// execDoneMsg is sent when the ansible-playbook process exits.
type execDoneMsg struct{ err error }

// ExecModel is a Bubble Tea model that shows a scrollable live log panel
// while an ansible-playbook subprocess is running.
//
// It can be embedded inside the launcher (withSidebar=true) or used
// standalone as a full-screen panel (withSidebar=false).
type ExecModel struct {
	// command is the display string shown in the header, e.g. "service start php83".
	command string

	// version string shown in the header.
	version string

	// withSidebar is true when embedded inside the TUI launcher.
	// Affects how the viewport width is calculated.
	withSidebar bool

	// proc is the running ansible-playbook subprocess.
	proc *exec.Cmd

	// logFile is the open debug.log handle seeked to EOF before the run starts.
	logFile *os.File

	// viewport displays the rolling log lines.
	vp viewport.Model

	// done is true once the subprocess has exited.
	done bool

	// err is non-nil if ansible exited with a non-zero status.
	err error

	// width/height of the available area (whole terminal for standalone,
	// right pane width for sidebar-embedded).
	width  int
	height int
}

// NewExecModel creates a new ExecModel ready to run.
// proc must already be started via ansible.RunSubprocess().
// width/height are the dimensions of the panel area.
func NewExecModel(command, version string, withSidebar bool, proc *exec.Cmd, width, height int) ExecModel {
	vp := viewport.New(viewport.WithWidth(width), viewport.WithHeight(execViewportHeight(height)))
	vp.SetContent("")

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
		vp:          vp,
		width:       width,
		height:      height,
	}
}

// SetSize updates the panel dimensions (called on terminal resize).
func (e ExecModel) SetSize(width, height int) ExecModel {
	e.width = width
	e.height = height
	e.vp.SetWidth(width)
	e.vp.SetHeight(execViewportHeight(height))
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
		// Poll log file for new lines.
		lines := e.readNewLogLines()
		if len(lines) > 0 {
			current := e.vp.GetContent()
			joined := strings.Join(lines, "\n")
			if current == "" {
				e.vp.SetContent(joined)
			} else {
				e.vp.SetContent(current + "\n" + joined)
			}
			e.vp.GotoBottom()
		}
		if !e.done {
			return e, tickCmd()
		}
		return e, nil

	case logLineMsg:
		current := e.vp.GetContent()
		line := string(msg)
		if current == "" {
			e.vp.SetContent(line)
		} else {
			e.vp.SetContent(current + "\n" + line)
		}
		e.vp.GotoBottom()
		return e, nil

	case execDoneMsg:
		// Do one final log drain before marking done.
		lines := e.readNewLogLines()
		if len(lines) > 0 {
			current := e.vp.GetContent()
			joined := strings.Join(lines, "\n")
			if current == "" {
				e.vp.SetContent(joined)
			} else {
				e.vp.SetContent(current + "\n" + joined)
			}
			e.vp.GotoBottom()
		}
		e.done = true
		e.err = msg.err
		if e.logFile != nil {
			_ = e.logFile.Close()
			e.logFile = nil
		}
		return e, nil

	case tea.KeyPressMsg:
		if e.done {
			// Any key press after completion exits.
			return e, tea.Quit
		}
		// Pass scroll keys through to viewport while running.
		var cmd tea.Cmd
		e.vp, cmd = e.vp.Update(msg)
		return e, cmd

	case tea.WindowSizeMsg:
		e = e.SetSize(msg.Width, msg.Height)
		return e, nil
	}

	var cmd tea.Cmd
	e.vp, cmd = e.vp.Update(msg)
	return e, cmd
}

// View renders the execution panel.
func (e ExecModel) View() string {
	var sb strings.Builder

	// Header: command being run.
	cmdLabel := styles.Header.Render("▶ valet.sh " + e.command)
	ver := styles.Version.Render("v" + e.version)
	pad := e.width - lipgloss.Width(cmdLabel) - lipgloss.Width(ver) - 2
	if pad < 1 {
		pad = 1
	}
	_, _ = fmt.Fprintln(&sb, cmdLabel+strings.Repeat(" ", pad)+ver)

	// Progress bar placeholder (to be implemented).
	_, _ = fmt.Fprintln(&sb, styles.Divider.Render(strings.Repeat("─", e.width)))

	// Log viewport.
	_, _ = fmt.Fprintln(&sb, e.vp.View())

	// Status footer.
	_, _ = fmt.Fprintln(&sb, styles.Divider.Render(strings.Repeat("─", e.width)))
	_, _ = fmt.Fprintln(&sb, e.statusLine())

	return sb.String()
}

// IsDone returns true once the subprocess has exited and the user has not
// yet dismissed the panel.
func (e ExecModel) IsDone() bool { return e.done }

// Err returns the subprocess exit error, if any.
func (e ExecModel) Err() error { return e.err }

// statusLine returns the status/hint line at the bottom of the panel.
func (e ExecModel) statusLine() string {
	if !e.done {
		return styles.HelpDesc.Render("⠋ running...   ↑/↓ scroll")
	}
	if e.err != nil {
		errLine := lipgloss.NewStyle().Foreground(colourRed).Bold(true).Render(
			"✘ failed — see " + logPath,
		)
		return errLine + "   " + styles.HelpDesc.Render("(press any key to exit)")
	}
	doneLine := styles.ItemSelected.Render("✔ done")
	return doneLine + "   " + styles.HelpDesc.Render("(press any key to exit)")
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

// execViewportHeight calculates the viewport height from available panel height.
func execViewportHeight(totalHeight int) int {
	h := totalHeight - execHeaderHeight - execFooterHeight
	if h < viewportMinHeight {
		h = viewportMinHeight
	}
	return h
}

// IsTTY reports whether stdout is connected to a terminal.
// Used by RunWithPanel to decide whether to show the TUI execution panel
// or fall back to direct ansible execution for non-interactive environments.
func IsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
