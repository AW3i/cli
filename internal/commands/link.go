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

func NewLinkCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "link",
		Short: "Link the current directory as an nginx vhost",
		Long:  "Creates an nginx virtual host for the project in the current directory, using the instance.key from .valet-sh.yml as the hostname.",
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

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

			return ansible.Run(ansible.RunOpts{
				Playbook: "link",
				WorkDir:  workDir,
				Verbose:  verbose,
			})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose Ansible output")
	return cmd
}

func NewUnlinkCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "unlink",
		Short: "Remove the nginx vhost for the current directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}

			return ansible.Run(ansible.RunOpts{
				Playbook: "unlink",
				WorkDir:  workDir,
				Verbose:  verbose,
			})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose Ansible output")
	return cmd
}

func NewLinksCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "links",
		Short: "List all active vhost links",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ansible.Run(ansible.RunOpts{
				Playbook: "links",
				Verbose:  verbose,
			})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose Ansible output")
	return cmd
}
