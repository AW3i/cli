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

// Package platform detects the current OS and CPU architecture, mirroring
// the logic in roles/shared-variables/tasks/main.yml.
package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// OS constants mirror the values used by the Ansible shared-variables role.
const (
	OSUbuntu = "ubuntu"
	OSMac    = "mac"
)

// Arch constants mirror the values used by the Ansible shared-variables role.
const (
	ArchAMD64 = "amd64"
	ArchARM64 = "arm64"
)

func detectOS() string {
	switch runtime.GOOS {
	case "darwin":
		return OSMac
	default:
		return OSUbuntu
	}
}

func detectArch() string {
	switch runtime.GOARCH {
	case "arm64":
		return ArchARM64
	default:
		return ArchAMD64
	}
}

// AnsiblePlaybookBin returns the full path to the ansible-playbook binary,
// preferring the valet-sh Python venv if present.
func AnsiblePlaybookBin() string {
	// The valet-sh installer creates a Python venv under /usr/local/valet-sh/venv.
	// Prefer that over whatever is on $PATH to ensure the correct Ansible version.
	candidates := []string{
		"/usr/local/valet-sh/venv/bin/ansible-playbook",
		"/usr/local/bin/ansible-playbook",
	}
	for _, c := range candidates {
		if isExecutable(c) {
			return c
		}
	}
	// Fall back to PATH lookup.
	if path, err := exec.LookPath("ansible-playbook"); err == nil {
		return path
	}
	return "ansible-playbook"
}

// repoDirDefault is the production install path of the valet-sh Ansible repo.
const repoDirDefault = "/usr/local/valet-sh/valet-sh"

// RepoDirEnvVar is the environment variable that overrides the Ansible repo
// path. Set it to the absolute path of your valet-sh checkout to run the
// development binary against local playbooks and config without installing:
//
//	export VALET_REPO_DIR=/home/user/workspace/valet-sh
//	./dist/valet service list
const RepoDirEnvVar = "VALET_REPO_DIR"

// RepoDir returns the root of the valet-sh Ansible repo.
// When VALET_REPO_DIR is set it overrides the default installed path, allowing
// the development binary to run against a local checkout without installing.
func RepoDir() string {
	if dir := os.Getenv(RepoDirEnvVar); dir != "" {
		return dir
	}
	return repoDirDefault
}

// DevRepoDir returns the VALET_REPO_DIR override if set, or empty string.
// Used to display a developer notice when running with a custom repo path.
func DevRepoDir() string {
	return os.Getenv(RepoDirEnvVar)
}

// LogFile returns the path to the ansible run log file.
func LogFile() string {
	return filepath.Join(RepoDir(), "log", "debug.log")
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&0o111 != 0
}
