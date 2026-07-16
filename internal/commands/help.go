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
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// ANSI codes — identical values to the Python callback plugin so that
// Go-layer output is visually consistent with Ansible task output.
const (
	ansiRed   = "\033[1;31m"
	ansiBlue  = "\033[1;34m"
	ansiGreen = "\033[0;32m"
	ansiBold  = "\033[;1m"
	ansiReset = "\033[0;0m"
)

// isTerminal reports whether w is connected to a TTY. When output is piped
// or redirected we skip all ANSI codes, matching fatih/color behavior
// without adding a dependency.
func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		fi, err := f.Stat()
		if err != nil {
			return false
		}
		return (fi.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

func blue(w io.Writer, s string) string {
	if isTerminal(w) {
		return ansiBlue + ansiBold + s + ansiReset
	}
	return s
}

func green(w io.Writer, s string) string {
	if isTerminal(w) {
		return ansiGreen + s + ansiReset
	}
	return s
}

// ErrorPrefix returns a styled "✘ msg" string for Go-layer error output.
// Used by root RunE (unknown command) and validation errors.
func ErrorPrefix(msg string) string {
	if isTerminal(os.Stderr) {
		return ansiRed + "✘ " + msg + ansiReset
	}
	return "✘ " + msg
}

// SetHelpFormatter installs a custom help renderer on the root command that
// cascades to all subcommands. Section headers get the blue ▶ prefix used
// by the Ansible callback's play_start output; command names are green.
func SetHelpFormatter(root *cobra.Command) {
	fn := helpFunc()
	root.SetHelpFunc(fn)
}

func helpFunc() func(*cobra.Command, []string) {
	return func(cmd *cobra.Command, _ []string) {
		w := cmd.OutOrStdout()

		// Long description (or short if no long).
		desc := cmd.Long
		if desc == "" {
			desc = cmd.Short
		}
		if desc != "" {
			_, _ = fmt.Fprintln(w, desc)
			_, _ = fmt.Fprintln(w)
		}

		// Usage line.
		if cmd.Runnable() || cmd.HasAvailableSubCommands() {
			_, _ = fmt.Fprintln(w, blue(w, "▶ Usage"))
			if cmd.Runnable() {
				_, _ = fmt.Fprintf(w, "  %s\n", cmd.UseLine())
			}
			if cmd.HasAvailableSubCommands() {
				_, _ = fmt.Fprintf(w, "  %s [command]\n", cmd.CommandPath())
			}
			_, _ = fmt.Fprintln(w)
		}

		// Subcommand listing.
		if cmd.HasAvailableSubCommands() {
			_, _ = fmt.Fprintln(w, blue(w, "▶ Available Commands"))
			for _, sub := range cmd.Commands() {
				if sub.IsAvailableCommand() {
					padding := strings.Repeat(" ", max(1, 14-len(sub.Name())))
					_, _ = fmt.Fprintf(w, "  %s%s%s\n",
						green(w, sub.Name()),
						padding,
						sub.Short,
					)
				}
			}
			_, _ = fmt.Fprintln(w)
		}

		// Flags.
		flags := cmd.LocalFlags()
		if cmd.HasAvailableLocalFlags() {
			_, _ = fmt.Fprintln(w, blue(w, "▶ Flags"))
			_, _ = fmt.Fprintln(w, flags.FlagUsages())
		}

		// Inherited flags (only shown if there are any beyond help).
		if cmd.HasAvailableInheritedFlags() {
			_, _ = fmt.Fprintln(w, blue(w, "▶ Global Flags"))
			_, _ = fmt.Fprintln(w, cmd.InheritedFlags().FlagUsages())
		}

		// Hint line.
		if cmd.HasAvailableSubCommands() {
			_, _ = fmt.Fprintf(w, "%s\n",
				blue(w, fmt.Sprintf(`Use "%s [command] --help" for more information about a command.`, cmd.CommandPath())),
			)
		}
	}
}


