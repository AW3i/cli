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

package platform

import (
	"testing"
)

func TestNormalizeServiceName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// PHP fuzzy aliases
		{"PHP8.3", "php83"},
		{"php8.3", "php83"},
		{"PHP83", "php83"},
		{"php83", "php83"},
		{"PHP7.4", "php74"},
		{"php7.4", "php74"},
		{"PHP5.6", "php56"},
		{"php5.6", "php56"},
		// MySQL/MariaDB aliases
		{"mysql5.7", "mysql57"},
		{"MYSQL5.7", "mysql57"},
		{"mysql8.0", "mysql80"},
		{"mariadb10.4", "mariadb104"},
		{"MARIADB10.4", "mariadb104"},
		{"mariadb10.6", "mariadb106"},
		{"mariadb10.11", "mariadb1011"},
		{"mariadb11.4", "mariadb114"},
		// Elasticsearch
		{"elasticsearch7", "elasticsearch7"},
		{"elasticsearch8", "elasticsearch8"},
		// OpenSearch
		{"opensearch2", "opensearch2"},
		// Already normalized
		{"php83", "php83"},
		{"redis", "redis"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeServiceName(tc.input)
			if got != tc.expected {
				t.Errorf("NormalizeServiceName(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestDetect(t *testing.T) {
	// This test just verifies Detect() returns a non-empty Info struct
	// The actual OS/arch values depend on the test runner environment
	info := Detect()

	if info.OS != OSUbuntu && info.OS != OSMac {
		t.Errorf("Detect() returned unexpected OS: %q", info.OS)
	}

	if info.Arch != ArchAMD64 && info.Arch != ArchARM64 {
		t.Errorf("Detect() returned unexpected Arch: %q", info.Arch)
	}
}
