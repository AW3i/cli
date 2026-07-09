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
	// valet_current_path is NOT passed as an extra-var. The playbook reads it
	// from the OLDPWD environment variable (set in setEnv below). Passing it as
	// an extra-var would give it Ansible's highest precedence (22), preventing
	// the playbook's set_fact calls from dynamically adjusting the path based
	// on instance.path in .valet-sh.yml. See link.yml:66-72 and load-valet-sh-file.yml:37.
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

	// Use syscall.Exec to deliver signals directly to ansible-playbook.
	// argv/env are from trusted sources (platform constants + CLI args).
	binPath, err := exec.LookPath(ansibleBin)
	if err != nil {
		// ansibleBin may already be an absolute path.
		binPath = ansibleBin
	}

	return syscall.Exec(binPath, argv, env)
}

// RunSubprocess starts ansible-playbook as a child process, returning a reader for
// its JSON output. Ansible's vars_prompt handles password input natively on the real
// stdin before the TUI starts. The caller gates BubbleTea on the first JSON task-start
// event (after vars_prompt completes). Returns the process, stdout reader, and cleanup func.
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
	}
	if extraVars.CLI.Args == nil {
		extraVars.CLI.Args = []string{}
	}
	if extraVars.CLI.Opts == nil {
		extraVars.CLI.Opts = []string{}
	}

	extraVarsJSON, err := json.Marshal(extraVars)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("serializing extra vars: %w", err)
	}

	ansibleBin := platform.AnsiblePlaybookBin()
	repoDir := platform.RepoDir()

	// Write extra-vars to a temp file to keep them out of ps aux output.
	evFile, err := os.CreateTemp("", "valetsh-vars-*")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating extra-vars file: %w", err)
	}
	evPath := evFile.Name()
	cleanup := func() { os.Remove(evPath) }

	if err := os.Chmod(evPath, 0600); err != nil {
		evFile.Close()
		cleanup()
		return nil, nil, nil, fmt.Errorf("setting extra-vars file permissions: %w", err)
	}
	if _, err := evFile.Write(extraVarsJSON); err != nil {
		evFile.Close()
		cleanup()
		return nil, nil, nil, fmt.Errorf("writing extra-vars file: %w", err)
	}
	if err := evFile.Close(); err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("closing extra-vars file: %w", err)
	}

	args := []string{playbookPath, "-e", "@" + evPath}
	if opts.Verbose {
		args = append(args, "-v")
	}

	env := os.Environ()
	env = setEnv(env, "OLDPWD", workDir)
	// Force Python to write stdout unbuffered so spinner lines arrive in real time.
	env = setEnv(env, "PYTHONUNBUFFERED", "1")

	cmd := exec.Command(ansibleBin, args...)
	cmd.Dir = repoDir
	cmd.Env = env

	// Pass stdin through so vars_prompt can read the password natively.
	cmd.Stdin = os.Stdin

	// Pipe stdout so the TUI can read task names in real time from JSON output.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = nil

	if err = cmd.Start(); err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("starting ansible-playbook: %w", err)
	}

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
