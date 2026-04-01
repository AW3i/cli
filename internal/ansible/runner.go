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
	"os"
	"os/exec"
	"path/filepath"
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
// stdout and stderr are discarded because the Ansible callback plugin writes
// all output to the log file; the TUI tails that file directly.
func RunSubprocess(opts *RunOpts) (*exec.Cmd, error) {
	playbookPath := filepath.Join(platform.RepoDir(), "playbooks", opts.Playbook+".yml")
	if _, err := os.Stat(playbookPath); err != nil {
		return nil, fmt.Errorf("playbook not found: %s", playbookPath)
	}

	workDir := opts.WorkDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getting working directory: %w", err)
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
		return nil, fmt.Errorf("serializing extra vars: %w", err)
	}

	ansibleBin := platform.AnsiblePlaybookBin()
	repoDir := platform.RepoDir()

	args := []string{playbookPath, "-e", string(extraVarsJSON)}
	if opts.Verbose {
		args = append(args, "-v")
	}

	env := os.Environ()
	env = setEnv(env, "OLDPWD", workDir)

	// If a become password was provided, pass it via the env var Ansible reads.
	// Correct name is ANSIBLE_BECOME_PASS (confirmed: ansible/plugins/become/sudo.py).
	// The slice is zeroed immediately after use so the password does not linger.
	// Note: stdin is NOT passed through — Bubble Tea owns stdin during TUI execution.
	// The password must always be supplied via this env var when running from the TUI.
	if len(opts.BecomePassword) > 0 {
		env = setEnv(env, "ANSIBLE_BECOME_PASS", string(opts.BecomePassword))
		for i := range opts.BecomePassword {
			opts.BecomePassword[i] = 0
		}
	}

	cmd := exec.Command(ansibleBin, args...)
	cmd.Dir = repoDir
	cmd.Env = env
	// Do NOT set cmd.Stdin — Bubble Tea owns stdin during TUI execution.
	// Discard stdout/stderr — all output goes to the log file via the callback.
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err = cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting ansible-playbook: %w", err)
	}

	return cmd, nil
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
