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

package main

import "testing"

func TestHasVIFlag(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"valet", "--vi"}, true},
		{[]string{"valet", "-vi"}, true},
		{[]string{"valet", "service"}, false},
		{[]string{"valet"}, false},
	}
	for _, tc := range tests {
		got := hasVIFlag(tc.args)
		if got != tc.want {
			t.Errorf("hasVIFlag(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestRemoveVIFlag(t *testing.T) {
	args := []string{"valet", "--vi", "service"}
	result := removeVIFlag(args)
	if len(result) != 2 {
		t.Fatalf("expected 2 args after removal, got %d: %v", len(result), result)
	}
	if result[1] != "service" {
		t.Errorf("expected 'service' at index 1, got %q", result[1])
	}
}

func TestRemoveVIFlagMultiple(t *testing.T) {
	args := []string{"valet", "-vi", "--vi", "service"}
	result := removeVIFlag(args)
	for _, a := range result {
		if a == "--vi" || a == "-vi" {
			t.Errorf("removeVIFlag left %q in result: %v", a, result)
		}
	}
}
