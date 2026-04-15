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

// Package ansible provides the subprocess runner that invokes ansible-playbook
// with the correct arguments and environment, mirroring what the valet.sh
// bash wrapper does today.
package ansible

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/valet-sh/cli/internal/platform"
)

// CLIVars is the structure passed to Ansible as the "cli" extra variable.
// It mirrors the convention established by the existing bash wrapper:
//
//	ansible-playbook playbooks/foo.yml -e '{"cli": {"args": [...], "opts": [...]}}'
type CLIVars struct {
	Args []string `json:"args"`
	Opts []string `json:"opts"`
}

// ExtraVars is the top-level extra-vars object passed to ansible-playbook.
type ExtraVars struct {
	CLI CLIVars `json:"cli"`
	// WorkDir is the user's current working directory at the time valet.sh was
	// invoked. Ansible playbooks read this via lookup('env','OLDPWD') today;
	// we pass it explicitly so the Go wrapper doesn't need the OLDPWD trick.
	WorkDir string `json:"valet_current_path,omitempty"`
	// BecomePassword, when non-empty, is passed as ansible_become_pass in
	// extra-vars. This suppresses vars_prompt in playbooks that declare:
	//   vars_prompt:
	//     - name: "ansible_become_pass"
	// Per Ansible source (playbook_executor.py): vars_prompt is skipped when
	// the variable is already present in extra_vars.
	BecomePassword string `json:"ansible_become_pass,omitempty"`
}

// RunOpts configures a single ansible-playbook invocation.
type RunOpts struct {
	// Playbook is the name without extension, e.g. "init-instance".
	Playbook string
	// Args are positional CLI arguments forwarded to the playbook (e.g. ["start", "php83"]).
	Args []string
	// Opts are flag-style CLI options forwarded to the playbook.
	Opts []string
	// WorkDir is the project directory; defaults to the process working directory.
	WorkDir string
	// Verbose enables ansible-playbook -v output.
	Verbose bool
	// BecomePassword is the sudo password for Ansible become tasks.
	// Only set when launching from the TUI — CLI mode uses stdin passthrough.
	// The slice is zeroed immediately after being written to the subprocess
	// environment so it does not linger in memory.
	BecomePassword []byte
}

// Run executes the given playbook as a subprocess, streaming stdout/stderr
// directly to the terminal. The process replaces the current process image
// (exec syscall) so signal handling is transparent — Ctrl-C reaches Ansible
// directly.
//
// If exec is not available (e.g. in tests), falls back to cmd.Run().
func Run(opts *RunOpts) error {
	playbookPath := filepath.Join(platform.RepoDir(), "playbooks", opts.Playbook+".yml")
	if _, err := os.Stat(playbookPath); err != nil {
		return fmt.Errorf("playbook not found: %s", playbookPath)
	}

	workDir := opts.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}
	}

	extraVars := ExtraVars{
		CLI: CLIVars{
			Args: opts.Args,
			Opts: opts.Opts,
		},
		WorkDir: workDir,
	}
	if extraVars.CLI.Args == nil {
		extraVars.CLI.Args = []string{}
	}
	if extraVars.CLI.Opts == nil {
		extraVars.CLI.Opts = []string{}
	}

	extraVarsJSON, err := json.Marshal(extraVars)
	if err != nil {
		return fmt.Errorf("serializing extra vars: %w", err)
	}

	ansibleBin := platform.AnsiblePlaybookBin()
	repoDir := platform.RepoDir()

	argv := []string{ansibleBin, playbookPath, "-e", string(extraVarsJSON)}
	if opts.Verbose {
		argv = append(argv, "-v")
	}

	// Change into the repo directory so ansible.cfg is picked up, exactly as
	// the current bash wrapper does with `cd $BASE_DIR`.
	if err = os.Chdir(repoDir); err != nil {
		return fmt.Errorf("chdir to %s: %w", repoDir, err)
	}

	// Preserve OLDPWD for playbooks that still use lookup('env','OLDPWD').
	env := os.Environ()
	env = setEnv(env, "OLDPWD", workDir)

	// Use syscall.Exec so that signals (SIGINT, SIGTERM) are delivered directly
	// to ansible-playbook and the valet process vanishes from the process table.
	//
	// SECURITY: This intentionally replaces the current process with ansible-playbook.
	// The argv and env are constructed from trusted sources (platform package constants
	// and user's CLI arguments). This is the same behavior as the original bash wrapper.
	binPath, err := exec.LookPath(ansibleBin)
	if err != nil {
		// ansibleBin may already be an absolute path.
		binPath = ansibleBin
	}

	return syscall.Exec(binPath, argv, env)
}

// RunSubprocess starts ansible-playbook as a child process without replacing
// the current process image. Unlike Run(), which uses syscall.Exec, this
// returns a started *exec.Cmd so the caller can wait on it and observe its
// exit status — used by the TUI execution panel which needs the Go process
// to stay alive for log tailing and rendering.
//
// The Ansible callback plugin writes spinner lines ("⠙ taskname\r") to stdout
// for every task start. RunSubprocess pipes that stdout so the TUI can read
// task names in real time without parsing the log file.
// stderr is discarded; structured output goes to the log file.
//
// Returns the started process, a reader for its stdout, and a cleanup func.
func RunSubprocess(opts *RunOpts) (*exec.Cmd, io.Reader, func(), error) {
	playbookPath := filepath.Join(platform.RepoDir(), "playbooks", opts.Playbook+".yml")
	if _, err := os.Stat(playbookPath); err != nil {
		return nil, nil, nil, fmt.Errorf("playbook not found: %s", playbookPath)
	}

	workDir := opts.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("getting working directory: %w", err)
		}
	}

	extraVars := ExtraVars{
		CLI: CLIVars{
			Args: opts.Args,
			Opts: opts.Opts,
		},
		WorkDir: workDir,
	}
	if extraVars.CLI.Args == nil {
		extraVars.CLI.Args = []string{}
	}
	if extraVars.CLI.Opts == nil {
		extraVars.CLI.Opts = []string{}
	}

	// Track temp files for cleanup.
	var tmpFiles []string
	cleanup := func() {
		for _, f := range tmpFiles {
			os.Remove(f)
		}
	}

	// --- Become password handling ---
	var becomePasswordFile string

	if len(opts.BecomePassword) > 0 {
		bpf, err := writeSecretFile(opts.BecomePassword)
		if err != nil {
			cleanup()
			return nil, nil, nil, fmt.Errorf("writing become-password-file: %w", err)
		}
		becomePasswordFile = bpf
		tmpFiles = append(tmpFiles, bpf)

		extraVars.BecomePassword = string(opts.BecomePassword)

		for i := range opts.BecomePassword {
			opts.BecomePassword[i] = 0
		}
	}

	extraVarsJSON, err := json.Marshal(extraVars)
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("serializing extra vars: %w", err)
	}

	ansibleBin := platform.AnsiblePlaybookBin()
	repoDir := platform.RepoDir()

	args := []string{playbookPath}

	evFile, err := writeSecretFile(extraVarsJSON)
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("writing extra-vars file: %w", err)
	}
	tmpFiles = append(tmpFiles, evFile)
	args = append(args, "-e", "@"+evFile)

	if becomePasswordFile != "" {
		args = append(args, "--become-password-file", becomePasswordFile)
	}

	if opts.Verbose {
		args = append(args, "-v")
	}

	env := os.Environ()
	env = setEnv(env, "OLDPWD", workDir)

	cmd := exec.Command(ansibleBin, args...)
	cmd.Dir = repoDir
	cmd.Env = env

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("opening /dev/null: %w", err)
	}
	cmd.Stdin = devNull

	// Pipe stdout so the TUI can read task names in real time.
	// The callback plugin prints "⠙ taskname\r" to stdout for each task start.
	// StdoutPipe must be called before Start; it returns a reader that is
	// automatically closed when cmd.Wait() is called by waitForProcess.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		devNull.Close()
		cleanup()
		return nil, nil, nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = nil

	if err = cmd.Start(); err != nil {
		devNull.Close()
		cleanup()
		return nil, nil, nil, fmt.Errorf("starting ansible-playbook: %w", err)
	}

	devNull.Close()

	return cmd, stdoutPipe, cleanup, nil
}

// ListTasks runs ansible-playbook --list-tasks to count how many tasks
// the playbook will execute. Returns 0 if listing fails so callers can
// fall back gracefully to a spinner without a total count.
//
// This is used by the TUI to render a real progress bar: [=====>    ] 12/47
// instead of just "12 tasks" with a spinner.
func ListTasks(opts *RunOpts) int {
	playbookPath := filepath.Join(platform.RepoDir(), "playbooks", opts.Playbook+".yml")
	if _, err := os.Stat(playbookPath); err != nil {
		return 0
	}

	workDir := opts.WorkDir
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	extraVars := ExtraVars{
		CLI: CLIVars{
			Args: opts.Args,
			Opts: opts.Opts,
		},
		WorkDir: workDir,
	}
	if extraVars.CLI.Args == nil {
		extraVars.CLI.Args = []string{}
	}
	if extraVars.CLI.Opts == nil {
		extraVars.CLI.Opts = []string{}
	}

	extraVarsJSON, err := json.Marshal(extraVars)
	if err != nil {
		return 0
	}

	ansibleBin := platform.AnsiblePlaybookBin()
	repoDir := platform.RepoDir()

	args := []string{playbookPath, "--list-tasks", "-e", string(extraVarsJSON)}
	if opts.Verbose {
		args = append(args, "-v")
	}

	cmd := exec.Command(ansibleBin, args...)
	cmd.Dir = repoDir
	cmd.Env = os.Environ()

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = nil

	if err = cmd.Run(); err != nil {
		return 0
	}

	return countTaskLines(output.String())
}

// countTaskLines counts lines in --list-tasks output that represent actual tasks.
//
// Ansible --list-tasks output format:
//
//	play #1 (localhost): Play name  TAGS: []
//
//	  task path: /path/to/file.yml:5
//	  Task name  TAGS: []
//
// We identify task lines by:
//   - Line starts with whitespace (indented)
//   - Line contains "TAGS:" (present on all task/play lines)
//   - Line does NOT contain "play #" (excludes play headers)
//   - Line does NOT contain "task path:" (excludes path metadata lines)
//
// This heuristic may miscount in edge cases, but it is good enough for a
// progress bar estimate. Returns 0 if the output is malformed.
func countTaskLines(output string) int {
	count := 0
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Must be indented and contain TAGS marker.
		if len(line) == 0 || line[0] != ' ' || !strings.Contains(trimmed, "TAGS:") {
			continue
		}

		// Skip play headers (they contain "play #").
		if strings.Contains(trimmed, "play #") {
			continue
		}

		// Skip "task path:" metadata lines.
		if strings.HasPrefix(trimmed, "task path:") {
			continue
		}

		count++
	}
	return count
}

// writeSecretFile writes data to a temp file with owner-only permissions (0600).
// The caller is responsible for removing the file when no longer needed.
func writeSecretFile(data []byte) (string, error) {
	tmpFile, err := os.CreateTemp("", "valetsh-secret-*")
	if err != nil {
		return "", err
	}

	if err := os.Chmod(tmpFile.Name(), 0600); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", err
	}

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", err
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

// setEnv sets or replaces a key in an environ slice (KEY=VALUE format).
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if len(e) >= len(prefix) && e[:len(prefix)] == prefix {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
