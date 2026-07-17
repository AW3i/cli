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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

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

	args := []string{playbookPath, "-e", string(extraVarsJSON)}
	if opts.Verbose {
		args = append(args, "-v")
	}

	env := os.Environ()
	env = setEnv(env, "OLDPWD", workDir)
	// Force Python to write stdout unbuffered so JSON events arrive in real time.
	env = setEnv(env, "PYTHONUNBUFFERED", "1")
	// Use the JSONL callback so the TUI receives structured events it can parse.
	// This overrides stdout_callback = valet-sh in ansible.cfg for TUI invocations only.
	env = setEnv(env, "ANSIBLE_STDOUT_CALLBACK", "ansible.posix.jsonl")

	// When -d / --debug is passed, enable verbose callback output.
	for _, opt := range opts.Opts {
		if opt == "-d" || opt == "--debug" {
			env = setEnv(env, "APPLICATION_DEBUG_INFO_ENABLED", "1")
			break
		}
	}

	// Open the log file and write a header. Truncated per run (matching the
	// Python callback's doRollover behaviour). Stderr is redirected here so
	// ansible startup errors (import failures, syntax errors) are captured too.
	var logFile *os.File
	logPath := platform.LogFile()
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err == nil {
		if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
			logFile = lf
			_, _ = fmt.Fprintf(logFile, "---------------------------------------------\n")
			_, _ = fmt.Fprintf(logFile, "Log started on %s.\n", time.Now().Format(time.ANSIC))
			_, _ = fmt.Fprintf(logFile, "---------------------------------------------\n\n")
		}
	}

	cleanup := func() {
		if logFile != nil {
			_ = logFile.Close()
		}
	}

	cmd := exec.Command(ansibleBin, args...)
	cmd.Dir = repoDir
	cmd.Env = env

	// Pass stdin through so vars_prompt can read the password natively.
	cmd.Stdin = os.Stdin

	// Pipe stdout so the TUI can read JSONL events in real time.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("creating stdout pipe: %w", err)
	}
	cmd.Stderr = logFile // nil if log file could not be opened — discards stderr

	if err = cmd.Start(); err != nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("starting ansible-playbook: %w", err)
	}

	return cmd, stdoutPipe, cleanup, nil
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
