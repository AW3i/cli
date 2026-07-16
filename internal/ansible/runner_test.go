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

// TestExtraVarsJSON verifies that ExtraVars marshals correctly to JSON.
// ansible_become_pass must never appear in the extra-vars JSON — Ansible's
// own vars_prompt handles the become password natively via stdin.
// valet_current_path is not passed as extra-var; playbooks read it from the
// OLDPWD environment variable, which allows set_fact to dynamically adjust
// the path based on instance.path in .valet-sh.yml.
func TestExtraVarsJSON(t *testing.T) {
	ev := ExtraVars{
		CLI: CLIVars{
			Args: []string{"start", "mysql80"},
			Opts: []string{},
		},
	}

	jsonBytes, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(jsonBytes)

	// Verify the expected routing fields are present.
	if !strings.Contains(jsonStr, `"cli"`) {
		t.Error("JSON missing 'cli' key")
	}

	// valet_current_path must NOT be in extra-vars; playbooks read it from
	// the OLDPWD environment variable. If it were in extra-vars (Ansible
	// precedence level 22), it would prevent the playbook's set_fact calls
	// (precedence level 19) from dynamically adjusting the path based on
	// instance.path from .valet-sh.yml.
	if strings.Contains(jsonStr, `"valet_current_path"`) {
		t.Error("JSON must not contain 'valet_current_path': it blocks playbook set_fact path adjustments")
	}

	// The become password must never appear in extra-vars: Ansible's
	// vars_prompt handles it natively via stdin on the raw terminal.
	if strings.Contains(jsonStr, `"ansible_become_pass"`) {
		t.Error("JSON must not contain 'ansible_become_pass': password handling belongs to Ansible vars_prompt, not extra-vars")
	}
}
