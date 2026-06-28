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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

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
// waitForFirstJSONTask() detects the first task-start JSON event from
// ansible.posix.jsonl — which fires only after vars_prompt has completed
// and tasks have begun — guaranteeing stdin is free before BubbleTea
// takes ownership.
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

	// Gate BubbleTea startup on the first task-start JSON event from
	// ansible.posix.jsonl. The jsonl callback emits play_start events
	// before vars_prompt fires; task-start events only appear after
	// vars_prompt has completed. All JSON lines consumed while waiting
	// are buffered and replayed so readTaskCmd() receives the full stream.
	taskOut := waitForFirstJSONTask(ansibleOut)

	commandStr := strings.Join(args, " ")
	return runExecPanel(commandStr, version, proc, taskOut, cleanup, 0)
}

// waitForFirstJSONTask reads ansible.posix.jsonl output line-by-line,
// buffering everything, until it finds a v2_playbook_on_task_start or
// v2_runner_on_start event. It then returns an io.Reader that replays
// all consumed bytes (including the task-start line) followed by the
// rest of the original reader.
//
// ansible.posix.jsonl emits events in this order per play:
//
//  1. v2_playbook_on_play_start → fires BEFORE vars_prompt
//  2. vars_prompt fires here    → Ansible reads password from stdin
//  3. v2_playbook_on_task_start → fires for each task AFTER vars_prompt
//
// Gating on the task-start event (step 3) ensures vars_prompt (step 2)
// has completed and stdin is free before BubbleTea takes over.
//
// If the pipe closes before any task starts (e.g. playbook syntax error),
// the buffered bytes are returned as-is so the exec panel shows the error.
func waitForFirstJSONTask(r io.Reader) io.Reader {
	var buf bytes.Buffer
	var line []byte
	oneByte := make([]byte, 1)

	for {
		n, err := r.Read(oneByte)
		if n > 0 {
			b := oneByte[0]
			buf.WriteByte(b)
			if b == '\n' {
				if isJSONTaskStart(line) {
					break
				}
				line = line[:0]
			} else {
				line = append(line, b)
			}
		}
		if err != nil {
			// Pipe closed before any task — return what we have.
			break
		}
	}

	return io.MultiReader(&buf, r)
}

// isJSONTaskStart returns true when line is a jsonl task-start event.
// Uses a fast bytes.Contains pre-check before JSON parsing to avoid
// allocating for every non-task line.
func isJSONTaskStart(line []byte) bool {
	if !bytes.Contains(line, []byte("v2_playbook_on_task_start")) &&
		!bytes.Contains(line, []byte("v2_runner_on_start")) {
		return false
	}
	var ev struct {
		Event string `json:"_event"`
	}
	return json.Unmarshal(line, &ev) == nil &&
		(ev.Event == "v2_playbook_on_task_start" || ev.Event == "v2_runner_on_start")
}

// runExecPanel starts a standalone Bubble Tea program showing the execution
// panel for the given proc. Called for direct CLI invocations (no sidebar).
// After the panel exits, any vsh_stdout output captured during the run
// (service tables, db listings, etc.) is printed to the terminal.
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
		// Print any vsh_stdout content (tables, listings) now that BubbleTea
		// has exited and the terminal is fully restored.
		if output := fm.execPanel.CapturedOutput(); output != "" {
			fmt.Println(output)
		}
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
