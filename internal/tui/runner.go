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
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/valet-sh/cli/internal/ansible"
)

// RunWithPanel executes a valet command via ansible-playbook and shows
// the live execution panel (header + spinner + log tail).
//
// It resolves the ansible RunOpts from the cobra command tree using the
// provided args slice (e.g. ["service", "start", "php83"]).
//
// Ansible's own vars_prompt handles the become-password prompt natively:
// stdin is connected to the subprocess so the user types directly into
// Ansible's prompt on the raw terminal. BubbleTea only takes over after
// waitForFirstTask() detects the first Braille spinner character from the
// callback plugin, which is only emitted once tasks start — guaranteeing
// that vars_prompt has completed and stdin is free.
//
// When stdout is not a TTY (CI, pipes), it falls back to the normal
// cobra/ansible path so non-interactive usage is never affected.
//
// Returns the ansible process exit error if the playbook failed, or nil.
func RunWithPanel(root *cobra.Command, args []string, version string) error {
	if !IsTTY() {
		// Non-interactive: delegate to cobra which calls ansible.Run (syscall.Exec).
		os.Args = append([]string{os.Args[0]}, args...)
		return root.Execute()
	}

	opts, err := resolveRunOpts(root, args)
	if err != nil {
		// Unknown command or bad args — let cobra print the error normally.
		os.Args = append([]string{os.Args[0]}, args...)
		return root.Execute()
	}

	proc, ansibleOut, cleanup, err := ansible.RunSubprocess(opts)
	if err != nil {
		return fmt.Errorf("starting ansible-playbook: %w", err)
	}

	// Gate BubbleTea startup on the first Braille spinner character in
	// Ansible's stdout. This ensures vars_prompt (the sudo password prompt)
	// has completed before BubbleTea takes over the terminal and stdin.
	//
	// All bytes consumed while waiting are buffered and prepended back onto
	// the reader so the exec panel receives the complete stdout stream.
	taskOut := waitForFirstTask(ansibleOut)

	// Wrap the stdout reader with a captureReader that intercepts \n-terminated
	// segments (vsh_stdout table output, db listings, etc.) while passing all
	// bytes through unchanged to the exec panel's task-name parser.
	// The captured content is printed to the terminal after the exec panel exits.
	cr := newCaptureReader(taskOut)

	commandStr := strings.Join(args, " ")
	err = runExecPanel(commandStr, version, proc, cr, cleanup, 0)

	// Print any captured vsh_stdout output (service tables, db listings, etc.)
	// now that BubbleTea has exited and the terminal is fully restored.
	if output := cr.Output(); len(output) > 0 {
		fmt.Fprint(os.Stdout, cleanControlCodes(output))
	}

	return err
}

// waitForFirstTask reads from the ansible stdout pipe byte-by-byte, buffering
// everything, until it detects the two-byte UTF-8 prefix of a Braille spinner
// character (\xe2\xa0). It then returns an io.Reader that replays all consumed
// bytes followed by the rest of the original reader.
//
// The valet-sh callback plugin writes to stdout in this order:
//
//  1. \033[?25h (CURSOR_SHOW) — at module import, first byte 0x1B
//  2. "▶ play-name\n" — at play start (before vars_prompt); '▶' is \xe2\x96\xb6
//  3. vars_prompt fires here — Ansible reads the password from os.Stdin
//  4. "\x1b[2K\r\033[0;32m⠙\033[0;0m taskname\r" — at each task start
//
// All Braille spinner characters (U+2800..U+28FF) encode as \xe2\xa0\x?? in
// UTF-8. The play-start '▶' (U+25B6) encodes as \xe2\x96\xb6. Checking the
// two-byte prefix \xe2\xa0 uniquely identifies a spinner character without
// false-matching the play-start line or any ANSI escape sequence.
//
// If the pipe closes before any task starts (e.g. playbook syntax error or
// early exit), the buffered bytes are returned as-is so the exec panel can
// still show the error state.
func waitForFirstTask(r io.Reader) io.Reader {
	var buf bytes.Buffer
	prev := byte(0)
	oneByte := make([]byte, 1)

	for {
		n, err := r.Read(oneByte)
		if n > 0 {
			b := oneByte[0]
			buf.WriteByte(b)

			// Braille spinner prefix: two consecutive bytes 0xE2 0xA0.
			// Read the completing third byte of the rune before breaking so the
			// exec model receives a valid UTF-8 sequence from the reader.
			if prev == 0xE2 && b == 0xA0 {
				n2, _ := r.Read(oneByte)
				if n2 > 0 {
					buf.WriteByte(oneByte[0])
				}
				break
			}
			prev = b
		}
		if err != nil {
			// Pipe closed (process exited before any task).
			break
		}
	}

	return io.MultiReader(&buf, r)
}

// captureReader wraps an io.Reader and intercepts the Ansible callback
// plugin's stdout stream to capture vsh_stdout content (service tables,
// db listings, etc.) while passing all bytes through unchanged to the
// exec panel's task-name parser.
//
// The callback plugin writes two categories of content to stdout:
//
//   - \r-terminated: spinner updates and FLUSH prefixes — task progress only.
//     These are consumed by readTaskCmd() for task-name display and discarded.
//
//   - \n-terminated: actual output (vsh_stdout), play-start decorators, and
//     stats (✔ done / ✘ failed). Only the actual output is captured; the
//     decorators and stats are filtered out because the exec panel handles
//     those itself.
//
// Filtering rule (applied after stripping ANSI escape codes):
//
//   - empty segment → discard
//   - starts with '▶' (play-start) → discard
//   - starts with '✔' (stats success) → discard
//   - starts with '✘' or 'ℹ' (stats failure / info) → discard
//   - starts with a Braille char (U+2800..U+28FF, spinner) → discard
//   - anything else → CAPTURE (vsh_stdout table content)
//
// All bytes are passed through Read() unchanged so readTaskCmd() continues
// to work normally. The captured content is retrieved after the exec panel
// exits via Output().
type captureReader struct {
	r       io.Reader
	pending []byte // accumulates the current incomplete line
	output  []byte // captured vsh_stdout segments (raw, with ANSI colors)
}

func newCaptureReader(r io.Reader) *captureReader {
	return &captureReader{r: r}
}

// Read passes bytes through to the caller and processes them for capture.
func (cr *captureReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.process(p[:n])
	}
	return n, err
}

// process classifies each byte and accumulates complete \n-terminated segments
// for capture evaluation.
func (cr *captureReader) process(b []byte) {
	for _, c := range b {
		switch c {
		case '\r':
			// Spinner / FLUSH overwrite line — discard accumulated pending bytes.
			cr.pending = cr.pending[:0]
		case '\n':
			// Complete \n-terminated segment — evaluate for capture.
			if cr.shouldCapture(cr.pending) {
				cr.output = append(cr.output, cr.pending...)
				cr.output = append(cr.output, '\n')
			}
			cr.pending = cr.pending[:0]
		default:
			cr.pending = append(cr.pending, c)
		}
	}
}

// shouldCapture returns true when a raw (ANSI-colored) line segment contains
// actual vsh_stdout content that the user should see printed after the exec panel.
func (cr *captureReader) shouldCapture(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}

	// Strip ANSI escape sequences to classify the content.
	stripped := strings.TrimSpace(ansiEscape.ReplaceAllString(string(raw), ""))
	if stripped == "" {
		return false
	}

	// Decode the first rune for classification.
	first, _ := utf8.DecodeRuneInString(stripped)
	if first == utf8.RuneError {
		return false
	}

	switch {
	case first == '▶':
		// play-start line ("▶ play-name") — decorative, discard.
		return false
	case first == '✔':
		// stats success ("✔ done") — exec panel handles this, discard.
		return false
	case first == '✘' || first == 'ℹ':
		// stats failure / info — exec panel handles errors, discard.
		return false
	case first >= 0x2800 && first <= 0x28FF:
		// Braille spinner character — task name line, discard.
		return false
	}

	// Everything else is vsh_stdout content (tables, listings, etc.).
	return true
}

// Output returns all captured vsh_stdout content accumulated so far.
// Safe to call after the exec panel exits (all stdout has been consumed).
func (cr *captureReader) Output() []byte {
	return cr.output
}

// cleanControlCodes removes terminal-control ANSI sequences that are harmless
// during playbook execution but would corrupt the terminal display when printed
// after the exec panel has already exited and restored the terminal:
//
//   - \x1b[2K  — FLUSH (erase current line): would clear the exec panel's last line
//   - \x1b[?25h — CURSOR_SHOW: cursor is already visible, no-op but noisy
//   - \x1b[?25l — CURSOR_HIDE: would hide the cursor unexpectedly
//
// SGR color codes (\x1b[...m) are preserved so tables render with their
// original colors in the user's terminal.
func cleanControlCodes(b []byte) string {
	s := string(b)
	s = strings.ReplaceAll(s, "\x1b[2K", "")
	s = strings.ReplaceAll(s, "\x1b[?25h", "")
	s = strings.ReplaceAll(s, "\x1b[?25l", "")
	return s
}

// runExecPanel starts a standalone Bubble Tea program showing the execution
// panel for the given proc. Called for direct CLI invocations (no sidebar).
func runExecPanel(command, version string, proc *exec.Cmd, ansibleOut io.Reader, cleanup func(), totalTasks int) error {
	// Get terminal size with fallback to 80x24.
	width, height := 80, 24
	if w, h, err := term.GetSize(os.Stdout.Fd()); err == nil {
		width, height = w, h
	}

	m := standaloneExecModel{
		execPanel: NewExecModel(command, version, false, proc, ansibleOut, cleanup, totalTasks, width, height),
	}

	p := tea.NewProgram(m)
	final, err := p.Run()
	if err != nil {
		return err
	}

	if fm, ok := final.(standaloneExecModel); ok {
		return fm.execPanel.Err()
	}
	return nil
}

// standaloneExecModel wraps ExecModel as a full standalone Bubble Tea program.
// Used for direct CLI invocations (no launcher sidebar).
type standaloneExecModel struct {
	execPanel ExecModel
}

func (standalone standaloneExecModel) Init() tea.Cmd {
	return standalone.execPanel.Init()
}

func (standalone standaloneExecModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	standalone.execPanel, cmd = standalone.execPanel.Update(msg)
	return standalone, cmd
}

func (standalone standaloneExecModel) View() tea.View {
	v := tea.NewView(standalone.execPanel.View())
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// resolveRunOpts walks the cobra command tree to find the matching command
// for the given args and builds an ansible.RunOpts from it.
// Separates positional arguments from flags: tokens starting with "-" go to Opts,
// everything else goes to Args.
func resolveRunOpts(root *cobra.Command, args []string) (*ansible.RunOpts, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("no command specified")
	}

	cmd, remaining, err := root.Find(args)
	if err != nil || cmd == root {
		return nil, fmt.Errorf("unknown command: %s", args[0])
	}

	// Prefer the canonical playbook name stored by discover.go in Annotations.
	// Falls back to the first word of cmd.Use for commands not built via Discover
	// (e.g. in tests).
	playbook := cmd.Annotations["playbook"]
	if playbook == "" {
		playbook = strings.SplitN(cmd.Use, " ", 2)[0]
	}

	// Separate positional args from opts (flags starting with "-").
	var positionalArgs, opts []string
	for _, token := range remaining {
		if strings.HasPrefix(token, "-") {
			opts = append(opts, token)
		} else {
			positionalArgs = append(positionalArgs, token)
		}
	}

	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}

	return &ansible.RunOpts{
		Playbook: playbook,
		Args:     positionalArgs,
		Opts:     opts,
		WorkDir:  workDir,
	}, nil
}
