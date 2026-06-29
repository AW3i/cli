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
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	// spinnerTickInterval is how often the spinner animation advances.
	spinnerTickInterval = 50 * time.Millisecond

	// taskLogPrefix is the string that begins a new Ansible task line in the log.
	// Used to count completed tasks for the progress indicator.
	taskLogPrefix = "TASK ["
)

// execTickMsg fires periodically to advance the spinner animation.
type execTickMsg time.Time

// execDoneMsg is sent when the ansible-playbook process exits.
type execDoneMsg struct{ err error }

// ansibleEventMsg carries one parsed event from the ansible.posix.jsonl stdout stream.
// A single message can carry a task-name update, formatted log lines, or an EOF signal.
type ansibleEventMsg struct {
	// taskName, when non-empty, updates the spinner's current task display.
	taskName string
	// logLines are formatted lines to append to the live log viewport.
	logLines []string
	// eof is true when the stdout pipe is closed — no more events will arrive.
	eof bool
}

// ExecModel is a Bubble Tea model that shows execution progress and a failure log viewer.
// It handles ansible-playbook subprocess execution, live task name updates, and
// post-failure log viewing.
type ExecModel struct {
	// command is the display string shown in the header, e.g. "service start php83".
	command string

	// version string shown in the header.
	version string

	// proc is the running ansible-playbook subprocess.
	proc *exec.Cmd

	// cleanup is a function that deletes temporary files created for the process.
	// Called in the execDoneMsg handler after the process exits.
	cleanup func()

	// ansibleOut is the reader connected to ansible-playbook's stdout pipe.
	// ansible.posix.jsonl writes one JSON line per event; readTaskCmd reads
	// task names from v2_playbook_on_task_start events and writes vsh_stdout
	// content directly to output (bypassing the BubbleTea message queue).
	ansibleOut io.Reader

	// output is a shared buffer for vsh_stdout content (tables, listings)
	// extracted from v2_runner_on_ok JSON events. Written by the readTaskCmd
	// goroutine directly — bypassing the BubbleTea message queue so that
	// tea.Quit cannot race ahead of ansibleOutputMsg delivery. The pointer
	// is shared across all value-copies of ExecModel (Elm architecture).
	// Read by runExecPanel() after p.Run() returns.
	output *bytes.Buffer

	// logLines accumulates all formatted log lines produced from JSON events.
	// Printed to stdout after BubbleTea exits if the user requested log view.
	logLines []string

	// tasksDone is the number of Ansible tasks completed so far, counted
	// by detecting "TASK [" lines in the accumulated log output.
	tasksDone int

	// currentTask is the human-readable name of the task currently being shown.
	// Extracted from JSON events. Always shows the most recently discovered
	// non-meta-task (what Ansible is currently doing).
	currentTask string

	// totalTasks is the total number of tasks that will be executed,
	// determined by ansible-playbook --list-tasks before the run.
	// Zero means the count is unknown (e.g. --list-tasks failed), so we
	// fall back to a simple spinner without a progress bar.
	totalTasks int

	// spinnerFrame is the current index into the spinner animation frames.
	// Advances on every execTickMsg while the process is running.
	spinnerFrame int

	// done is true once the subprocess has exited.
	done bool

	// err is non-nil if ansible exited with a non-zero status.
	err error

	// awaitingLogPrompt is true when we have shown the "View full log? [Y/n]"
	// prompt and are waiting for the user's answer.
	awaitingLogPrompt bool

	// logViewRequested is true once the user has said Y and the program should
	// quit and display the log. After BubbleTea exits, the log is printed to stdout
	// with native terminal scrolling and selection support.
	logViewRequested bool

	// stdoutEOF is true once readTaskCmd has returned ansibleEventMsg{eof:true},
	// meaning the stdout pipe has been fully drained and all vsh_stdout
	// content has been written to the output buffer. Used to coordinate
	// the quit decision when ansibleEventMsg{eof:true} arrives before execDoneMsg.
	stdoutEOF bool

	// width/height of the available area.
	width  int
	height int
}

// NewExecModel creates a new ExecModel ready to run.
// proc must already be started via ansible.RunSubprocess().
// cleanup is a function that deletes temporary files (passwords, extra-vars) after the process exits.
// totalTasks is the total number of tasks (from --list-tasks), or 0 if unknown.
// width/height are the dimensions of the panel area.
// output is a shared *bytes.Buffer that readTaskCmd writes vsh_stdout content
// to directly (bypassing the BubbleTea message queue). The caller reads from
// it after p.Run() returns to print tables/listings to the terminal.
func NewExecModel(command, version string, proc *exec.Cmd, ansibleOut io.Reader, output *bytes.Buffer, cleanup func(), totalTasks, width, height int) ExecModel {
	return ExecModel{
		command:    command,
		version:    version,
		proc:       proc,
		ansibleOut: ansibleOut,
		output:     output,
		cleanup:    cleanup,
		totalTasks: totalTasks,
		width:      width,
		height:     height,
	}
}

// SetSize updates the panel dimensions (called on terminal resize).
func (e ExecModel) SetSize(width, height int) ExecModel {
	e.width = width
	e.height = height
	return e
}

// Init starts the spinner ticker, the process waiter, and the stdout event reader.
func (e ExecModel) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		waitForProcess(e.proc),
		readTaskCmd(e.ansibleOut, e.output),
	)
}

// Update handles events, tick, process exit, and key presses.
func (e ExecModel) Update(msg tea.Msg) (ExecModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ansibleEventMsg:
		if msg.eof {
			// Stdout pipe fully drained — all vsh_stdout content is in the buffer.
			e.stdoutEOF = true
			// Quit in CLI success mode only when execDoneMsg has also arrived.
			// If execDoneMsg arrived first: done=true → quit now.
			// If this arrives first: done=false → set flag, execDoneMsg will quit.
			if e.done && e.err == nil {
				return e, tea.Quit
			}
			return e, nil
		}
		if msg.taskName != "" {
			e.currentTask = msg.taskName
		}
		if len(msg.logLines) > 0 {
			e.appendLines(msg.logLines)
		}
		return e, readTaskCmd(e.ansibleOut, e.output)

	case execTickMsg:
		if !e.done {
			e.spinnerFrame++
			return e, tickCmd()
		}
		return e, nil

	case execDoneMsg:
		e.done = true
		e.err = msg.err
		// Clean up temporary files (password file, extra-vars file).
		if e.cleanup != nil {
			e.cleanup()
		}
		// Quit in CLI success mode only after stdout is fully drained.
		// If stdoutEOF is already true (ansibleEventMsg{eof:true} arrived first):
		// safe to quit now — buffer is complete.
		// If stdoutEOF is false (pipe not yet drained):
		// don't quit yet — ansibleEventMsg{eof:true} handler will quit when it fires.
		if e.err == nil && e.stdoutEOF {
			return e, tea.Quit
		}
		// On failure: user must press a key first, then we show the prompt.
		return e, nil

	case tea.KeyPressMsg:
		return e.handleKey(msg)

	case tea.WindowSizeMsg:
		e = e.SetSize(msg.Width, msg.Height)
		return e, nil
	}

	// All other states consume the message but don't update anything.
	return e, nil
}

// handleKey processes keyboard input for all exec sub-states.
func (e ExecModel) handleKey(msg tea.KeyPressMsg) (ExecModel, tea.Cmd) {
	key := msg.String()

	// Awaiting Y/n prompt after failure.
	if e.awaitingLogPrompt {
		switch key {
		case "y", "Y", "enter":
			e.awaitingLogPrompt = false
			e.logViewRequested = true
			return e, tea.Quit
		case "n", "esc", "q", "ctrl+c":
			return e, tea.Quit
		}
		// Silently ignore all other keys while prompting.
		return e, nil
	}

	// Still running — handle ctrl+c only.
	if !e.done {
		if key == "ctrl+c" {
			// Signal the subprocess to terminate.
			if e.proc != nil && e.proc.Process != nil {
				_ = e.proc.Process.Signal(os.Interrupt)
			}
			// Clean up temp files immediately (prevent double-call from execDoneMsg).
			if e.cleanup != nil {
				e.cleanup()
				e.cleanup = nil
			}
			return e, tea.Quit
		}
		// Silently ignore other keys while running.
		return e, nil
	}

	// Done with error — show prompt on first key press (unless it's ctrl+c or y/Y).
	if e.err != nil && !e.awaitingLogPrompt {
		if key == "ctrl+c" || key == "esc" || key == "q" {
			return e, tea.Quit
		}
		// If user presses y/Y, skip the prompt and request log view.
		if key == "y" || key == "Y" {
			e.logViewRequested = true
			return e, tea.Quit
		}
		// Any other key: show the prompt and wait for response.
		e.awaitingLogPrompt = true
		return e, nil
	}

	// Done (success) — any key exits.
	return e, tea.Quit
}

// View renders the CLI view.
func (e ExecModel) View() string {
	return e.cliView()
}

// cliView renders the minimal CLI view: command header, spinner line, and optional error prompt.
func (e ExecModel) cliView() string {
	var output strings.Builder

	// Header: command being run + version.
	cmdLabel := styles.Header.Render("▶ valet.sh " + e.command)
	versionLabel := styles.Version.Render("v" + e.version)
	versionPadding := e.width - lipgloss.Width(cmdLabel) - lipgloss.Width(versionLabel) - 2
	if versionPadding < 1 {
		versionPadding = 1
	}
	_, _ = fmt.Fprintln(&output, cmdLabel+strings.Repeat(" ", versionPadding)+versionLabel)

	// Spinner line: shows current task while running, checkmark/cross when done.
	_, _ = fmt.Fprintln(&output, e.progressBarView())

	// On error: show hint or prompt.
	if e.done && e.err != nil {
		if e.awaitingLogPrompt {
			promptLine := styles.HelpDesc.Render("View full log? ") +
				styles.HelpKey.Render("[Y]") +
				styles.HelpDesc.Render("/") +
				styles.ItemDim.Render("n")
			_, _ = fmt.Fprint(&output, promptLine)
		} else {
			hintLine := styles.HelpDesc.Render("(press y to view log, q to exit)")
			_, _ = fmt.Fprint(&output, hintLine)
		}
	}

	return output.String()
}

// logViewerView renders the full-screen log viewer.
// spinnerFrames are the animation frames for the progress spinner.
// Each frame is displayed for one logPollInterval (100ms).
// Used as a fallback when totalTasks is unknown.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// progressBarView renders the progress indicator line between the header and
// the log viewport. Shows a spinner with current task while running, checkmark or
// cross when done.
func (e ExecModel) progressBarView() string {
	if e.done {
		if e.err != nil {
			return lipgloss.NewStyle().Foreground(colourRed).Render(
				"✘  " + fmt.Sprintf("%d tasks", e.tasksDone),
			)
		}
		return styles.ItemSelected.Render(
			"✔  " + fmt.Sprintf("%d tasks completed", e.tasksDone),
		)
	}

	// Always show spinner while running.
	frame := spinnerFrames[e.spinnerFrame%len(spinnerFrames)]
	spinner := styles.HelpKey.Render(frame)

	// Show current task if available, otherwise show "running..."
	taskDisplay := e.currentTask
	if taskDisplay == "" {
		taskDisplay = "running..."
	} else {
		// Shorten the task name to show only the description part (after | or :)
		taskDisplay = shortTaskName(taskDisplay)
	}
	counter := styles.HelpDesc.Render("  " + taskDisplay)
	return spinner + counter
}

// statusLines returns the footer content — one or two lines depending on state.
func (e ExecModel) statusLines() string {
	if !e.done {
		return styles.HelpDesc.Render("⠋ running...   ↑/↓ scroll   C copy")
	}

	if e.err != nil {
		failLine := lipgloss.NewStyle().Foreground(colourRed).Bold(true).Render(
			"✘ failed",
		)
		if e.awaitingLogPrompt {
			promptLine := styles.HelpDesc.Render("View full log? ") +
				styles.HelpKey.Render("[Y]") +
				styles.HelpDesc.Render("/") +
				styles.ItemDim.Render("n")
			return failLine + "\n" + promptLine
		}
		return failLine + "\n" + styles.HelpDesc.Render("(press y to view log, C to copy, any other key to exit)")
	}

	return styles.ItemSelected.Render("✔ done") +
		"   " + styles.HelpDesc.Render("(press C to copy or any key to exit)")
}

// IsDone returns true once the subprocess has exited.
func (e ExecModel) IsDone() bool { return e.done }

// Err returns the subprocess exit error, if any.
func (e ExecModel) Err() error { return e.err }

// LogViewRequested returns true if the user requested to view the full log.
// After BubbleTea exits, the caller should display the log via printLogView().
func (e ExecModel) LogViewRequested() bool { return e.logViewRequested }

// LogLines returns the accumulated formatted log lines.
// Typically used after BubbleTea exits to display the log via printLogView().
func (e ExecModel) LogLines() []string { return e.logLines }

// copyToClipboardOSC52 copies the given text to the system clipboard using the
// OSC 52 escape sequence. This works in modern terminals like iTerm2, Kitty,
// WezTerm, Alacritty, and tmux (with set-clipboard on). If the terminal doesn't
// support OSC 52, this is silently ignored.
//
// Format: \033]52;c;<base64-encoded-text>\007
func copyToClipboardOSC52(text string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	oscSeq := "\033]52;c;" + encoded + "\007"
	_, _ = os.Stdout.WriteString(oscSeq)
}

// appendLine appends a single line to the logLines accumulator,
// and advances the task counter when a TASK-prefix line is seen.
func (e *ExecModel) appendLine(line string) {
	if strings.HasPrefix(line, taskLogPrefix) {
		e.tasksDone++
	}
	e.logLines = append(e.logLines, line)
}

// appendLines appends multiple lines to the logLines accumulator,
// counting TASK-prefix lines to advance the task counter.
func (e *ExecModel) appendLines(lines []string) {
	for _, line := range lines {
		if strings.HasPrefix(line, taskLogPrefix) {
			e.tasksDone++
		}
	}
	e.logLines = append(e.logLines, lines...)
}

// tickCmd returns a tea.Cmd that fires after spinnerTickInterval.
func tickCmd() tea.Cmd {
	return tea.Tick(spinnerTickInterval, func(t time.Time) tea.Msg {
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

// openLogViewerCmd delivers a logViewReadyMsg from the already-accumulated
// logLines slice. No file I/O — the lines were built from JSON events in-process.
// readTaskCmd returns a tea.Cmd that blocks reading JSON lines from the
// ansible.posix.jsonl stdout stream and returns an ansibleEventMsg for each
// parsed event:
//
//   - v2_playbook_on_task_start / v2_runner_on_start → taskName + TASK log line
//   - v2_runner_on_ok with vsh_stdout → written to out buffer (no BubbleTea msg)
//   - v2_runner_on_ok with stderr/stdout → warning log lines in ansibleEventMsg
//   - v2_runner_on_failed / v2_runner_on_unreachable → error log lines
//   - v2_playbook_on_stats → recap log lines
//   - EOF / pipe closed → ansibleEventMsg{eof: true}
//
// vsh_stdout is written to the shared buffer directly (bypassing BubbleTea)
// so that tea.Quit cannot race ahead of buffer writes in CLI success mode.
// Reads byte-by-byte so no bytes are lost between successive goroutine calls.
func readTaskCmd(r io.Reader, out *bytes.Buffer) tea.Cmd {
	if r == nil {
		return nil
	}
	return func() tea.Msg {
		var line []byte
		oneByte := make([]byte, 1)
		for {
			n, err := r.Read(oneByte)
			if n > 0 {
				if oneByte[0] == '\n' {
					if msg := parseJSONEvent(line, out); msg != nil {
						return msg
					}
					line = line[:0]
				} else {
					line = append(line, oneByte[0])
				}
			}
			if err != nil {
				return ansibleEventMsg{eof: true}
			}
		}
	}
}

// ansibleHostResult holds the per-host fields we extract from jsonl events.
type ansibleHostResult struct {
	VshStdout   string          `json:"vsh_stdout"`
	Msg         string          `json:"msg"`
	Stderr      string          `json:"stderr"`
	Stdout      string          `json:"stdout"`
	Failed      bool            `json:"failed"`
	Unreachable bool            `json:"unreachable"`
	RC          int             `json:"rc"`
	Cmd         json.RawMessage `json:"cmd"`
}

// ansibleJSONEvent is the schema for ansible.posix.jsonl output lines.
// Each line is a complete JSON object with an _event field identifying the hook.
type ansibleJSONEvent struct {
	Event string `json:"_event"`
	Task  struct {
		Name string `json:"name"`
	} `json:"task"`
	Hosts map[string]ansibleHostResult `json:"hosts"`
	// Stats is populated for v2_playbook_on_stats events.
	Stats map[string]struct {
		Ok          int `json:"ok"`
		Failures    int `json:"failures"`
		Unreachable int `json:"unreachable"`
		Changed     int `json:"changed"`
		Skipped     int `json:"skipped"`
	} `json:"stats"`
}

// parseJSONEvent parses a single jsonl line and returns an ansibleEventMsg,
// or nil to continue reading without a BubbleTea round-trip.
//
// vsh_stdout content is written directly to out (bypasses BubbleTea queue).
// All other displayable content is returned in ansibleEventMsg.logLines.
func parseJSONEvent(line []byte, out *bytes.Buffer) tea.Msg {
	if len(line) == 0 || line[0] != '{' {
		return nil
	}
	var ev ansibleJSONEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return nil
	}

	switch ev.Event {
	case "v2_playbook_on_task_start", "v2_runner_on_start":
		name := strings.TrimSpace(ev.Task.Name)
		if name == "" {
			return nil
		}
		taskLine := taskLogPrefix + name + "] " + strings.Repeat("*", 20)
		// Meta-tasks (include_tasks, import_tasks, etc.) are logged but do not
		// update the spinner — they execute instantly and the real work happens
		// in the included file.
		if isMetaTask(name) {
			return ansibleEventMsg{logLines: []string{taskLine}}
		}
		return ansibleEventMsg{
			taskName: shortTaskName(name),
			logLines: []string{taskLine},
		}

	case "v2_runner_on_ok":
		var logLines []string
		for _, result := range ev.Hosts {
			// vsh_stdout goes directly to the shared buffer (bypasses BubbleTea).
			if result.VshStdout != "" && out != nil {
				if out.Len() > 0 {
					out.WriteByte('\n')
				}
				out.WriteString(result.VshStdout)
			}
			// stderr / stdout on ok = warnings — show in log.
			logLines = append(logLines, formatWarningLines(ev.Task.Name, result)...)
		}
		if len(logLines) > 0 {
			return ansibleEventMsg{logLines: logLines}
		}
		return nil

	case "v2_runner_on_failed":
		var logLines []string
		for _, result := range ev.Hosts {
			logLines = append(logLines, formatFailureLines(ev.Task.Name, result)...)
		}
		if len(logLines) > 0 {
			return ansibleEventMsg{logLines: logLines}
		}
		return nil

	case "v2_runner_on_unreachable":
		var logLines []string
		for _, result := range ev.Hosts {
			logLines = append(logLines, "UNREACHABLE ["+ev.Task.Name+"]")
			if result.Msg != "" {
				logLines = append(logLines, "  msg: "+result.Msg)
			}
		}
		if len(logLines) > 0 {
			return ansibleEventMsg{logLines: logLines}
		}
		return nil

	case "v2_playbook_on_stats":
		var logLines []string
		logLines = append(logLines, strings.Repeat("─", 60))
		logLines = append(logLines, "PLAY RECAP")
		for host, s := range ev.Stats {
			logLines = append(logLines, fmt.Sprintf(
				"  %-20s ok=%-4d changed=%-4d failed=%-4d unreachable=%-4d skipped=%-4d",
				host, s.Ok, s.Changed, s.Failures, s.Unreachable, s.Skipped,
			))
		}
		return ansibleEventMsg{logLines: logLines}
	}

	return nil
}

// formatWarningLines builds log lines for a successful task that emitted
// stderr or stdout (i.e. warnings from the module).
func formatWarningLines(taskName string, r ansibleHostResult) []string {
	stderr := strings.TrimSpace(r.Stderr)
	stdout := strings.TrimSpace(r.Stdout)
	if stderr == "" && stdout == "" {
		return nil
	}
	var lines []string
	lines = append(lines, "WARNING ["+taskName+"]")
	if stderr != "" {
		lines = append(lines, "  stderr:")
		for _, l := range strings.Split(stderr, "\n") {
			lines = append(lines, "    "+l)
		}
	}
	if stdout != "" {
		lines = append(lines, "  stdout:")
		for _, l := range strings.Split(stdout, "\n") {
			lines = append(lines, "    "+l)
		}
	}
	return lines
}

// formatFailureLines builds the detailed error block for a failed task.
// Includes msg, rc, cmd, stderr, and stdout so developers can diagnose
// failures (e.g. composer errors) without leaving the TUI.
func formatFailureLines(taskName string, r ansibleHostResult) []string {
	var lines []string
	lines = append(lines, "FAILED ["+taskName+"]")
	if r.Msg != "" {
		lines = append(lines, "  msg: "+r.Msg)
	}
	if r.RC != 0 {
		lines = append(lines, fmt.Sprintf("  rc:  %d", r.RC))
	}
	if cmd := formatCmd(r.Cmd); cmd != "" {
		lines = append(lines, "  cmd: "+cmd)
	}
	if stderr := strings.TrimSpace(r.Stderr); stderr != "" {
		lines = append(lines, "  stderr:")
		for _, l := range strings.Split(stderr, "\n") {
			lines = append(lines, "    "+l)
		}
	}
	if stdout := strings.TrimSpace(r.Stdout); stdout != "" {
		lines = append(lines, "  stdout:")
		for _, l := range strings.Split(stdout, "\n") {
			lines = append(lines, "    "+l)
		}
	}
	return lines
}

// formatCmd renders the cmd field (string or []string) as a single string.
func formatCmd(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return strings.Join(arr, " ")
	}
	return string(raw)
}

// parseLogTaskName extracts the task name from a log line like:
// "TASK [role-name : task-name] ****..."
// Returns the part between brackets, or empty string if not found.
func parseLogTaskName(line string) string {
	start := strings.Index(line, "[")
	if start == -1 {
		return ""
	}
	end := strings.Index(line, "]")
	if end == -1 || end <= start {
		return ""
	}
	return strings.TrimSpace(line[start+1 : end])
}

// logViewerViewportHeight calculates the log viewer viewport height.
// IsTTY reports whether stdout is connected to a terminal.
func IsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// isMetaTask returns true if the task name represents an Ansible meta-task
// that controls flow (include_tasks, import_tasks, etc.) rather than doing real work.
// These tasks execute instantly and should not be shown as "current task" since
// the real work happens in the included/imported file.
func isMetaTask(taskName string) bool {
	// Extract the task name part (after the last " : " if role-qualified)
	base := taskName
	if i := strings.LastIndex(taskName, " : "); i >= 0 {
		base = taskName[i+3:]
	}

	// Check if it's a meta-task
	switch base {
	case "include_tasks", "import_tasks", "include_role", "import_role":
		return true
	}
	return false
}

// shortTaskName extracts the meaningful description part of a task name
// by removing the role prefix and keeping only the task description.
//
// Examples:
//
//	"valet-init-instance : workflows » magento2 » services » php | ensure php8.3 is started"
//	→ "ensure php8.3 is started"
//
//	"shared-variables : set 'current_os' var"
//	→ "set 'current_os' var"
//
//	"Gathering Facts"
//	→ "Gathering Facts"
func shortTaskName(taskName string) string {
	// If the task has a pipe separator, use what comes after it
	// (e.g., "role : workflows » ... | task description" → "task description")
	if i := strings.LastIndex(taskName, " | "); i >= 0 {
		return strings.TrimSpace(taskName[i+3:])
	}

	// No pipe — try to strip the role prefix (e.g., "role : task" → "task")
	if i := strings.Index(taskName, " : "); i >= 0 {
		return strings.TrimSpace(taskName[i+3:])
	}

	// No role prefix (e.g., "Gathering Facts" or "set variables")
	return taskName
}
