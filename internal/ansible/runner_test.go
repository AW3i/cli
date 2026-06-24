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

package ansible

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCountTaskLines(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected int
	}{
		{
			name:     "empty output",
			output:   "",
			expected: 0,
		},
		{
			name:     "single play no tasks",
			output:   "  play #1 (localhost): Install  TAGS: []",
			expected: 0,
		},
		{
			name: "one task",
			output: `  play #1 (localhost): Install  TAGS: []

    task path: /path/to/task.yml:1
    Gathering Facts  TAGS: []`,
			expected: 1,
		},
		{
			name: "multiple tasks",
			output: `  play #1 (localhost): Service  TAGS: []

    task path: /path/to/task.yml:1
    Start php-fpm  TAGS: []
    task path: /path/to/task.yml:5
    Enable php-fpm  TAGS: []
    task path: /path/to/task.yml:9
    Restart nginx  TAGS: []`,
			expected: 3,
		},
		{
			name: "task with colon in name",
			output: `  play #1: Install

    Install package: nginx  TAGS: []`,
			expected: 1,
		},
		{
			name: "header lines without indentation are skipped",
			output: `playbook: test.yml
  play #1: Test  TAGS: []
    task path: /path/to/task.yml:1
    First task  TAGS: []

Playbook without indentation should be skipped`,
			expected: 1,
		},
		{
			name: "multiple plays with tasks",
			output: `  play #1: First  TAGS: []

    Task A  TAGS: []

  play #2: Second  TAGS: []

    Task B  TAGS: []
    Task C  TAGS: []`,
			expected: 3,
		},
		{
			name: "task path lines are excluded",
			output: `  play #1: Test  TAGS: []

    task path: /some/path.yml:10
    Real task  TAGS: []
    task path: /another/path.yml:20
    Another task  TAGS: []`,
			expected: 2,
		},
		{
			name: "unnamed tasks (just module names)",
			output: `  play #1: Test  TAGS: []

    service  TAGS: []
    command  TAGS: []
    copy  TAGS: []`,
			expected: 3,
		},
		{
			name: "realistic ansible --list-tasks output",
			output: `playbook: /usr/local/valet-sh/valet-sh/playbooks/service.yml

  play #1 (localhost): Manage valet.sh services  TAGS: []

    task path: /usr/local/valet-sh/valet-sh/roles/service/tasks/main.yml:1
    Gathering Facts  TAGS: []
    task path: /usr/local/valet-sh/valet-sh/roles/service/tasks/main.yml:5
    Include OS-specific variables  TAGS: []
    task path: /usr/local/valet-sh/valet-sh/roles/service/tasks/main.yml:10
    Start service  TAGS: []`,
			expected: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := countTaskLines(tc.output)
			if result != tc.expected {
				t.Errorf("countTaskLines() = %d, want %d", result, tc.expected)
			}
		})
	}
}

// TestExtraVarsJSON verifies that ExtraVars marshals correctly to JSON,
// particularly that ansible_become_pass is included with the correct key.
func TestExtraVarsJSON(t *testing.T) {
	ev := ExtraVars{
		CLI: CLIVars{
			Args: []string{"start", "mysql80"},
			Opts: []string{},
		},
		WorkDir:        "/home/user/project",
		BecomePassword: "secret123",
	}

	jsonBytes, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(jsonBytes)

	// Verify the structure contains expected keys.
	if !strings.Contains(jsonStr, `"cli"`) {
		t.Error("JSON missing 'cli' key")
	}
	if !strings.Contains(jsonStr, `"valet_current_path"`) {
		t.Error("JSON missing 'valet_current_path' key")
	}
	if !strings.Contains(jsonStr, `"ansible_become_pass"`) {
		t.Error("JSON missing 'ansible_become_pass' key - vars_prompt will not be suppressed!")
	}

	// Verify the password value is present (checking for a substring is sufficient).
	if !strings.Contains(jsonStr, `"secret123"`) {
		t.Error("JSON missing the become password value")
	}
}

// TestExtraVarsJSONOmitEmpty verifies that ansible_become_pass is omitted
// when empty, per the omitempty tag.
func TestExtraVarsJSONOmitEmpty(t *testing.T) {
	ev := ExtraVars{
		CLI: CLIVars{
			Args: []string{"db", "ls"},
			Opts: []string{},
		},
		WorkDir:        "/home/user/project",
		BecomePassword: "", // empty - should be omitted
	}

	jsonBytes, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(jsonBytes)

	// ansible_become_pass should NOT be present when empty.
	if strings.Contains(jsonStr, `"ansible_become_pass"`) {
		t.Error("JSON should not contain 'ansible_become_pass' when empty, but it does")
	}
}
