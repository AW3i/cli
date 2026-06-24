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
	"github.com/valet-sh/cli/internal/platform"
)

func NewServiceCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "service <action> [service-name]",
		Short: "Manage valet-sh services",
		Long: `Start, stop, restart, enable, disable, or set the default for a service.

Actions:
  start    <service>   Start a service
  stop     <service>   Stop a service
  restart  <service>   Restart a service
  enable   <service>   Enable a service (start on boot / valet.sh install)
  disable  <service>   Disable a service
  default  <service>   Set a service as the scope default (e.g. default PHP version)
  list                 List all services and their current state

Services (examples):
  php83, php82, php81 ...
  mariadb114, mariadb1011, mariadb106, mariadb104
  mysql84, mysql80, mysql57
  elasticsearch8, elasticsearch7 ... opensearch3, opensearch2, opensearch1
  redis, valkey8, rabbitmq, nginx`,
		Args: requireArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Normalize the service name alias (e.g. "PHP8.3" → "php83") if
			// a service name was provided as the second argument.
			normalized := make([]string, len(args))
			copy(normalized, args)
			if len(normalized) > 1 {
				normalized[1] = platform.NormalizeServiceName(normalized[1])
			}

			return ansible.Run(&ansible.RunOpts{
				Playbook: "service",
				Args:     normalized,
				Verbose:  verbose,
			})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose Ansible output")
	return cmd
}
