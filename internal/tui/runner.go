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
	"io"
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/valet-sh/cli/internal/ansible"
)

// promptPassword prints a password prompt to stderr and reads a masked
// password from stdin. Called before BubbleTea takes over the terminal.
// The returned bytes do not include the trailing newline.
func promptPassword() ([]byte, error) {
	fmt.Fprint(os.Stderr, "[sudo] Password: ")
	password, err := term.ReadPassword(os.Stdin.Fd())
	fmt.Fprintln(os.Stderr) // restore newline after hidden input
	return password, err
}

// RunWithPanel executes a valet command via ansible-playbook and shows
// the live execution panel (header + progress placeholder + log tail).
//
// It resolves the ansible RunOpts from the cobra command tree using the
// provided args slice (e.g. ["service", "start", "php83"]).
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

	// Collect sudo password from the terminal before BubbleTea takes over.
	// The password is then written to a secure temp file and passed to Ansible
	// via --become-password-file and in extra-vars (to suppress vars_prompt).
	password, err := promptPassword()
	if err != nil {
		return fmt.Errorf("reading password: %w", err)
	}
	opts.BecomePassword = password

	proc, ansibleOut, cleanup, err := ansible.RunSubprocess(opts)
	if err != nil {
		return fmt.Errorf("starting ansible-playbook: %w", err)
	}

	commandStr := strings.Join(args, " ")
	return runExecPanel(commandStr, version, proc, ansibleOut, cleanup, 0)
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

	// The cobra Use field contains the command name as the first word.
	playbook := strings.SplitN(cmd.Use, " ", 2)[0]

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
