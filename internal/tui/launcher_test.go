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

package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
)

// noop is a minimal RunE that satisfies cobra's "available" check.
func noop(_ *cobra.Command, _ []string) error { return nil }

// testRoot builds a minimal cobra command tree for testing.
// Commands must have a RunE to be considered "available" by cobra.
func testRoot() *cobra.Command {
	root := &cobra.Command{Use: "valet", Short: "test root", RunE: noop}

	install := &cobra.Command{Use: "install", Short: "Install services", RunE: noop}
	root.AddCommand(install)

	service := &cobra.Command{Use: "service <action> [service-name]", Short: "Manage services"}
	service.AddCommand(&cobra.Command{Use: "start", Short: "Start a service", RunE: noop})
	service.AddCommand(&cobra.Command{Use: "stop", Short: "Stop a service", RunE: noop})
	root.AddCommand(service)

	project := &cobra.Command{Use: "project", Short: "Project operations"}
	project.AddCommand(&cobra.Command{Use: "env", Short: "Regenerate env.php", RunE: noop})
	project.AddCommand(&cobra.Command{Use: "cc", Short: "Clear cache", RunE: noop})
	root.AddCommand(project)

	return root
}

func TestNewModel(t *testing.T) {
	root := testRoot()
	m := newModel(root, "1.0.0")

	if len(m.stack) != 1 {
		t.Fatalf("expected 1 stack entry, got %d", len(m.stack))
	}
	if m.stack[0].title != "valet.sh" {
		t.Errorf("expected root title 'valet.sh', got %q", m.stack[0].title)
	}
	if m.activeScreen != screenList {
		t.Errorf("expected screenList on init, got %v", m.activeScreen)
	}
}

func TestQuitKeys(t *testing.T) {
	root := testRoot()
	m := newModel(root, "1.0.0")

	for _, key := range []string{"q", "ctrl+c"} {
		result, cmd := m.Update(tea.KeyPressMsg{Code: rune(0), Text: key})
		_ = result
		if cmd == nil {
			t.Errorf("key %q: expected Quit command", key)
		}
	}
}

func TestEscAtRootQuitsModel(t *testing.T) {
	root := testRoot()
	m := newModel(root, "1.0.0")

	// Esc at root level should queue a Quit.
	_, cmd := m.Update(tea.KeyPressMsg{Text: "esc"})
	if cmd == nil {
		t.Error("Esc at root: expected Quit command")
	}
}

func TestWindowResize(t *testing.T) {
	root := testRoot()
	m := newModel(root, "1.0.0")

	result, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	rm := result.(model)

	if rm.width != 120 {
		t.Errorf("expected width 120, got %d", rm.width)
	}
	if rm.height != 40 {
		t.Errorf("expected height 40, got %d", rm.height)
	}
}

func TestCommandItemTitle(t *testing.T) {
	cmd := &cobra.Command{Use: "service <action> [name]", Short: "Manage services"}
	item := CommandItem{Cmd: cmd}

	if item.Title() != "service" {
		t.Errorf("expected 'service', got %q", item.Title())
	}
}

func TestCommandItemBackTitle(t *testing.T) {
	item := CommandItem{IsBack: true}
	if item.Title() != "← back" {
		t.Errorf("expected '← back', got %q", item.Title())
	}
}

func TestCommandItemHasSubCommands(t *testing.T) {
	parent := &cobra.Command{Use: "service", Short: "Manage services"}
	parent.AddCommand(&cobra.Command{Use: "start", Short: "Start", RunE: noop})

	item := CommandItem{Cmd: parent}
	if !item.HasSubCommands() {
		t.Error("expected HasSubCommands true for command with available children")
	}

	leaf := &cobra.Command{Use: "install", Short: "Install", RunE: noop}
	leafItem := CommandItem{Cmd: leaf}
	if leafItem.HasSubCommands() {
		t.Error("expected HasSubCommands false for leaf command")
	}
}

func TestArgsFromUse(t *testing.T) {
	tests := []struct {
		use      string
		wantLen  int
		wantReq  []bool
		wantName []string
	}{
		{"install", 0, nil, nil},
		{"service <action> [service-name]", 2, []bool{true, false}, []string{"action", "service-name"}},
		{"xdebug <on|off> [php-version]", 2, []bool{true, false}, []string{"on|off", "php-version"}},
		{"db <action> [args...]", 1, []bool{true}, []string{"action"}}, // variadic excluded
		{"restore [identifier]", 1, []bool{false}, []string{"identifier"}},
	}

	for _, tc := range tests {
		t.Run(tc.use, func(t *testing.T) {
			defs := argsFromUse(tc.use)
			if len(defs) != tc.wantLen {
				t.Fatalf("argsFromUse(%q): want %d defs, got %d", tc.use, tc.wantLen, len(defs))
			}
			for i, def := range defs {
				if def.Required != tc.wantReq[i] {
					t.Errorf("def[%d].Required: want %v, got %v", i, tc.wantReq[i], def.Required)
				}
				if def.Name != tc.wantName[i] {
					t.Errorf("def[%d].Name: want %q, got %q", i, tc.wantName[i], def.Name)
				}
			}
		})
	}
}

func TestArgPaneIsReady(t *testing.T) {
	defs := []ArgDef{
		{Name: "action", Required: true},
		{Name: "name", Required: false},
	}
	pane := NewArgPane(defs, 80)

	// Initially not ready — required field is empty.
	if pane.IsReady() {
		t.Error("expected IsReady false before filling required field")
	}
}

func TestArgPaneIsReadyNoArgs(t *testing.T) {
	pane := NewArgPane(nil, 80)
	if !pane.IsReady() {
		t.Error("expected IsReady true when no required args")
	}
}

func TestCommandPath(t *testing.T) {
	root := testRoot()
	m := newModel(root, "1.0.0")

	// Find the service command by name (cobra sorts commands alphabetically).
	var serviceCmd *cobra.Command
	for _, c := range root.Commands() {
		if c.Use == "service <action> [service-name]" {
			serviceCmd = c
			break
		}
	}
	if serviceCmd == nil {
		t.Fatal("service command not found in test root")
	}

	serviceItem := CommandItem{Cmd: serviceCmd}
	updatedModel, _ := m.pushStack(serviceItem)
	um := updatedModel.(model)

	// Find the start subcommand.
	var startCmd *cobra.Command
	for _, c := range serviceCmd.Commands() {
		if c.Use == "start" {
			startCmd = c
			break
		}
	}
	if startCmd == nil {
		t.Fatal("start subcommand not found")
	}

	startItem := CommandItem{Cmd: startCmd}
	um.selectedItem = &startItem

	path := commandPath(um)
	if len(path) != 2 {
		t.Fatalf("expected path length 2, got %d: %v", len(path), path)
	}
	if path[0] != "service" {
		t.Errorf("expected path[0]='service', got %q", path[0])
	}
	if path[1] != "start" {
		t.Errorf("expected path[1]='start', got %q", path[1])
	}
}

func TestItemsFromCommands(t *testing.T) {
	root := testRoot()
	cmds := root.Commands()

	items := itemsFromCommands(cmds, false)
	if len(items) != len(cmds) {
		t.Errorf("expected %d items, got %d", len(cmds), len(items))
	}

	withBack := itemsFromCommands(cmds, true)
	if len(withBack) != len(cmds)+1 {
		t.Errorf("expected %d items with back, got %d", len(cmds)+1, len(withBack))
	}
	back, ok := withBack[0].(CommandItem)
	if !ok || !back.IsBack {
		t.Error("expected first item to be back item")
	}
}

func TestWordWrap(t *testing.T) {
	tests := []struct {
		text     string
		maxWidth int
		wantLen  int // number of lines
	}{
		{"hello world", 20, 1},
		{"hello world", 5, 2},
		{"a b c d e f", 3, 3}, // "a b" fits on 3 chars, same for "c d" and "e f"
		{"", 20, 0},
	}

	for _, tc := range tests {
		wrapped := wordWrap(tc.text, tc.maxWidth)
		if tc.text == "" {
			if wrapped != "" {
				t.Errorf("wordWrap(%q, %d): want empty, got %q", tc.text, tc.maxWidth, wrapped)
			}
			continue
		}
		lines := 1
		for _, ch := range wrapped {
			if ch == '\n' {
				lines++
			}
		}
		if lines != tc.wantLen {
			t.Errorf("wordWrap(%q, %d): want %d lines, got %d", tc.text, tc.maxWidth, tc.wantLen, lines)
		}
	}
}
