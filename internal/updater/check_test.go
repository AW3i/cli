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

package updater

import (
	"testing"
	"os"
)

func TestUpdateChannel(t *testing.T) {
	tests := []struct {
		env  string
		want string
	}{
		{"", "stable"},
		{"stable", "stable"},
		{"STABLE", "stable"},
		{"dev", "dev"},
		{"DEV", "dev"},
		{"Dev", "dev"},
		{"latest", "stable"}, // unrecognised → stable
		{"prerelease", "stable"},
	}

	for _, tc := range tests {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv(UpdateChannelEnvVar, tc.env)
			got := updateChannel()
			if got != tc.want {
				t.Errorf("updateChannel() with %s=%q = %q, want %q",
					UpdateChannelEnvVar, tc.env, got, tc.want)
			}
		})
	}

	// Unset env should default to stable.
	t.Run("unset", func(t *testing.T) {
		os.Unsetenv(UpdateChannelEnvVar)
		if got := updateChannel(); got != "stable" {
			t.Errorf("updateChannel() unset = %q, want %q", got, "stable")
		}
	})
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input    string
		expected [3]int
	}{
		{"2.9.19", [3]int{2, 9, 19}},
		{"2.9.19-101-gabcdef", [3]int{2, 9, 19}},
		{"3.0.0", [3]int{3, 0, 0}},
		{"2.10.0", [3]int{2, 10, 0}},
		{"2.9", [3]int{2, 9, 0}},
		{"2", [3]int{2, 0, 0}},
		{"", [3]int{0, 0, 0}},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := parseSemver(tc.input)
			if result != tc.expected {
				t.Errorf("parseSemver(%q) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"2.9.20", "2.9.19", true},
		{"2.9.19", "2.9.19", false},
		{"2.9.18", "2.9.19", false},
		{"3.0.0", "2.9.19", true},
		{"2.10.0", "2.9.19", true},
		{"2.9.20", "2.9.19-101-gabcdef", true},
		{"2.10.5", "2.9.30", true},
		{"1.0.0", "2.0.0", false},
		// Note: pre-release versions (2.0.0-beta) are parsed as 2.0.0
		// This is intentional - we compare the base versions only
	}

	for _, tc := range tests {
		t.Run(tc.candidate+"_vs_"+tc.current, func(t *testing.T) {
			got := isNewer(tc.candidate, tc.current)
			if got != tc.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}

func TestIsHelpOrVersionCall(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"help flag", []string{"valet", "--help"}, true},
		{"h flag", []string{"valet", "-h"}, true},
		{"version flag", []string{"valet", "--version"}, true},
		{"v flag", []string{"valet", "-v"}, true},
		{"help subcommand", []string{"valet", "help"}, true},
		{"service with help", []string{"valet", "service", "-h"}, true},
		{"normal command", []string{"valet", "service", "start", "php83"}, false},
		{"install", []string{"valet", "install"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsHelpOrVersionCall(tc.args)
			if got != tc.want {
				t.Errorf("IsHelpOrVersionCall(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}
