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
	"runtime"
	"strings"
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

// Info holds the detected platform information passed to Ansible as extra vars.
type Info struct {
	// OS is "ubuntu" or "mac" — matches current_os in Ansible.
	OS string
	// Arch is "amd64" or "arm64" — matches current_arch in Ansible.
	Arch string
}

// Detect returns the current platform information.
// On Linux it always reports "ubuntu" (matching current valet-sh behavior;
// Linux Mint remapping is handled inside the Ansible role).
func Detect() Info {
	return Info{
		OS:   detectOS(),
		Arch: detectArch(),
	}
}

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

// PlaybookDir returns the absolute path to the valet-sh playbooks directory.
func PlaybookDir() string {
	return "/usr/local/valet-sh/valet-sh/playbooks"
}

// RepoDir returns the root of the installed valet-sh Ansible repo.
func RepoDir() string {
	return "/usr/local/valet-sh/valet-sh"
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&0o111 != 0
}

// NormalizeServiceName applies the fuzzy alias mapping that the Ansible
// valet-service role performs, e.g. "PHP8.3" → "php83", "mariadb10.4" → "mariadb104".
// Returns the canonical service name unchanged if no alias matches.
func NormalizeServiceName(name string) string {
	lower := strings.ToLower(name)

	// Replace dots only in the version part (after the service prefix).
	// Mapping is derived from valet_sh_service_fuzzy_alias_mapping in
	// roles/shared-variables/defaults/main/valet-service.yml.
	aliases := map[string]string{
		"php5.6": "php56", "php7.0": "php70", "php7.1": "php71",
		"php7.2": "php72", "php7.3": "php73", "php7.4": "php74",
		"php8.0": "php80", "php8.1": "php81", "php8.2": "php82",
		"php8.3": "php83", "php8.4": "php84", "php8.5": "php85",
		"mysql5.7": "mysql57", "mysql8.0": "mysql80", "mysql8.4": "mysql84",
		"mariadb10.4": "mariadb104", "mariadb10.6": "mariadb106",
		"mariadb10.11": "mariadb1011", "mariadb11.4": "mariadb114",
		"elasticsearch1": "elasticsearch1", "elasticsearch2": "elasticsearch2",
		"elasticsearch5": "elasticsearch5", "elasticsearch6": "elasticsearch6",
		"elasticsearch7": "elasticsearch7", "elasticsearch8": "elasticsearch8",
		"opensearch1": "opensearch1", "opensearch2": "opensearch2",
		"opensearch3": "opensearch3",
	}

	if canonical, ok := aliases[lower]; ok {
		return canonical
	}
	return lower
}
