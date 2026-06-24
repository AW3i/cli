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

func NewXdebugCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "xdebug <on|off> [php-version]",
		Short: "Toggle Xdebug for a PHP version",
		Long: `Enable or disable Xdebug for the specified PHP version (or the project's
configured PHP version if run inside a project directory).

Examples:
  valet.sh xdebug on
  valet.sh xdebug off php83`,
		Args: requireArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return ansible.Run(&ansible.RunOpts{
				Playbook: "xdebug",
				Args:     args,
				Verbose:  verbose,
			})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose Ansible output")
	return cmd
}
