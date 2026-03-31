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
	"github.com/valet-sh/cli/internal/config"
)

func NewInitInstanceCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "init-instance [project-name]",
		Short: "Bootstrap a project instance from .valet-sh.yml",
		Long: `Reads the .valet-sh.yml in the current directory (or clones the project
from the configured hub if a project name is provided), then starts all
required services and runs the project-type-specific bootstrap workflow
(Magento 2, Magento 1, Neos, AEM, OroCRM).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			// When no project name is given, validate .valet-sh.yml early so
			// the user gets a clear Go error instead of an Ansible assertion failure.
			if len(args) == 0 {
				cfg, err := config.LoadProject(workDir)
				if err != nil {
					return err
				}
				if validationErrors := cfg.Validate(); len(validationErrors) > 0 {
					fmt.Fprintln(os.Stderr, ErrorPrefix("invalid .valet-sh.yml:"))
					for _, e := range validationErrors {
						fmt.Fprintf(os.Stderr, "  - %s\n", e)
					}
					return fmt.Errorf("fix the errors above and try again")
				}
			}

			return ansible.Run(&ansible.RunOpts{
				Playbook: "init-instance",
				Args:     args,
				WorkDir:  workDir,
				Verbose:  verbose,
			})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose Ansible output")
	return cmd
}
