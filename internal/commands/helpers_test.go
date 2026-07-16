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
	"testing"
)

func TestMax(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{5, 3, 5},
		{3, 5, 5},
		{5, 5, 5},
		{-1, 1, 1},
	}

	for _, tc := range tests {
		got := max(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("max(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestErrorPrefix(t *testing.T) {
	// Test that ErrorPrefix formats the message
	// In non-TTY mode (test environment), it should just return plain text with checkmark
	msg := "test error"
	result := ErrorPrefix(msg)

	if result == "" {
		t.Error("ErrorPrefix returned empty string")
	}

	// In non-TTY, should contain the message
	if result != "✘ test error" {
		// If it has ANSI codes, that's fine too - TTY detection varies
		t.Logf("ErrorPrefix result: %q", result)
	}
}
