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
	"github.com/spf13/cobra"
	"github.com/valet-sh/cli/internal/ansible"
)

func NewInitCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a blank .valet-sh.yml in the current directory",
		Long:  "Generates a .valet-sh.yml template in the current working directory.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return ansible.Run(&ansible.RunOpts{
				Playbook: "init",
				Args:     args,
				Verbose:  verbose,
			})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose Ansible output")
	return cmd
}
