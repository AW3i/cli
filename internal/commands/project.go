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
	"github.com/valet-sh/cli/internal/ansible"
)

// NewProjectCmd returns the "project" parent command with env and cc subcommands.
// The colon-separated original names (project:env, project:cc) are exposed as
// subcommands of "project" for ergonomics, while the underlying playbook names
// remain unchanged.
func NewProjectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Project-level operations",
	}

	cmd.AddCommand(newProjectEnvCmd())
	cmd.AddCommand(newProjectCCCmd())
	return cmd
}

func newProjectEnvCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "env",
		Short: "Regenerate app/etc/env.php for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			return ansible.Run(&ansible.RunOpts{
				Playbook: "project:env",
				Args:     args,
				WorkDir:  workDir,
				Verbose:  verbose,
			})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose Ansible output")
	return cmd
}

func newProjectCCCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "cc",
		Short: "Clear the Magento cache for the current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			return ansible.Run(&ansible.RunOpts{
				Playbook: "project:cc",
				Args:     args,
				WorkDir:  workDir,
				Verbose:  verbose,
			})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose Ansible output")
	return cmd
}
