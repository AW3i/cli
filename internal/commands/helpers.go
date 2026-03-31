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
)

// requireArgs returns a cobra.PositionalArgs validator that shows the
// command's own help when fewer than min arguments are provided, and a
// clean error (no stack trace, no usage dump) when too many are given.
//
// This replaces cobra.RangeArgs / cobra.MinimumNArgs on every subcommand so
// that:
//
//	valet.sh service            → shows `valet service --help`
//	valet.sh service start      → runs normally
//	valet.sh service a b c d    → prints a short error, exits 1
func requireArgs(minArgs, maxArgs int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < minArgs {
			// Print help to stdout (same as --help) and exit 0 — the user
			// just didn't know the syntax, not an error worth a non-zero exit.
			_ = cmd.Help()
			os.Exit(0)
		}
		if maxArgs >= 0 && len(args) > maxArgs {
			return fmt.Errorf("accepts between %d and %d argument(s), received %d", minArgs, maxArgs, len(args))
		}
		return nil
	}
}

// requireMinArgs is a convenience wrapper for commands with no upper bound.
func requireMinArgs(minArgs int) cobra.PositionalArgs {
	return requireArgs(minArgs, -1)
}
