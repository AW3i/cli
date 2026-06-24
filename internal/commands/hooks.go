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

package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/valet-sh/cli/internal/config"
)

// playbooksWithProjectValidation lists the playbook names that require a
// valid .valet-sh.yml in the current directory before Ansible is invoked.
//
// The Go-side pre-flight check catches missing/invalid config early —
// before sudo prompt and Ansible startup — and reports clean, actionable
// errors instead of cryptic Ansible task failures.
var playbooksWithProjectValidation = map[string]bool{
	"link":          true,
	"init-instance": true,
}

// ApplyHooks walks the discovered command list and attaches PreRunE hooks
// to commands that need them. Call this after Discover() and before
// registering commands on the root cobra command.
func ApplyHooks(cmds []*cobra.Command) {
	for _, cmd := range cmds {
		applyHooksRecursive(cmd)
	}
}

// applyHooksRecursive attaches hooks to cmd and all its subcommands.
func applyHooksRecursive(cmd *cobra.Command) {
	playbook := cmd.Annotations["playbook"]
	if playbooksWithProjectValidation[playbook] {
		cmd.PreRunE = validateProjectConfig
	}
	for _, sub := range cmd.Commands() {
		applyHooksRecursive(sub)
	}
}

// validateProjectConfig is a cobra PreRunE hook that reads and validates the
// .valet-sh.yml file in the current working directory before the command runs.
//
// It surfaces clear, structured errors (missing fields, unsupported types,
// conflicting services) instead of letting Ansible emit cryptic task failures
// after the sudo prompt and startup overhead.
func validateProjectConfig(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	cfg, err := config.LoadProject(workDir)
	if err != nil {
		return err
	}

	if errs := cfg.Validate(); len(errs) > 0 {
		fmt.Fprintln(os.Stderr, ErrorPrefix("invalid .valet-sh.yml:"))
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		return fmt.Errorf("fix the errors above and try again")
	}

	return nil
}
