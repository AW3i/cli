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
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestExecModelInit(t *testing.T) {
	m := NewExecModel("service start php83", "1.0.0", false, nil, 80, 24)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil Cmd (tick + wait)")
	}
}

func TestExecModelDoneOnExecDoneMsg(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)

	rm, _ := m.Update(execDoneMsg{err: nil})

	if !rm.IsDone() {
		t.Error("expected IsDone() true after execDoneMsg")
	}
	if rm.Err() != nil {
		t.Errorf("expected nil error, got %v", rm.Err())
	}
}

func TestExecModelFailedExecDoneMsg(t *testing.T) {
	m := NewExecModel("service start php83", "1.0.0", false, nil, 80, 24)
	sentinel := errors.New("exit status 1")

	rm, _ := m.Update(execDoneMsg{err: sentinel})

	if !rm.IsDone() {
		t.Error("expected IsDone() true after failed execDoneMsg")
	}
	if rm.Err() != sentinel {
		t.Errorf("expected sentinel error, got %v", rm.Err())
	}
}

func TestExecModelQuitOnKeyWhenDone(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)

	// Mark as done first.
	done, _ := m.Update(execDoneMsg{err: nil})

	// Any key press when done should trigger tea.Quit.
	_, cmd := done.Update(tea.KeyPressMsg{Text: "enter"})
	if cmd == nil {
		t.Error("expected Quit cmd on keypress after done")
	}
}

func TestExecModelNoQuitOnKeyWhileRunning(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)

	// While still running, keys should scroll viewport, not quit.
	rm, _ := m.Update(tea.KeyPressMsg{Text: "down"})
	if rm.IsDone() {
		t.Error("should not be done after scroll key")
	}
}

func TestExecModelWindowResize(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)

	rm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if rm.width != 120 {
		t.Errorf("expected width 120, got %d", rm.width)
	}
	if rm.height != 40 {
		t.Errorf("expected height 40, got %d", rm.height)
	}
}

func TestExecModelSetSize(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)
	m = m.SetSize(120, 40)

	if m.width != 120 {
		t.Errorf("expected width 120, got %d", m.width)
	}
}

func TestExecModelLogLineAppended(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)

	rm, _ := m.Update(logLineMsg("TASK [install] ok"))

	content := rm.vp.GetContent()
	if content != "TASK [install] ok" {
		t.Errorf("expected log line in viewport, got %q", content)
	}
}

func TestExecModelMultipleLogLines(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)

	m2, _ := m.Update(logLineMsg("line 1"))
	m3, _ := m2.Update(logLineMsg("line 2"))

	content := m3.vp.GetContent()
	if content != "line 1\nline 2" {
		t.Errorf("expected two lines, got %q", content)
	}
}

func TestExecViewportHeight(t *testing.T) {
	tests := []struct {
		totalHeight int
		wantMin     int
	}{
		{24, viewportMinHeight},
		{30, 30 - execHeaderHeight - execFooterHeight},
		{10, viewportMinHeight},
	}

	for _, tc := range tests {
		got := execViewportHeight(tc.totalHeight)
		expected := tc.totalHeight - execHeaderHeight - execFooterHeight
		if expected < viewportMinHeight {
			expected = viewportMinHeight
		}
		if got != expected {
			t.Errorf("execViewportHeight(%d) = %d, want %d", tc.totalHeight, got, expected)
		}
		if got < viewportMinHeight {
			t.Errorf("execViewportHeight(%d) = %d, below minimum %d", tc.totalHeight, got, viewportMinHeight)
		}
	}
}

func TestResolveRunOpts(t *testing.T) {
	root := testRoot()

	opts, err := resolveRunOpts(root, []string{"install"})
	if err != nil {
		t.Fatalf("resolveRunOpts: unexpected error: %v", err)
	}
	if opts.Playbook != "install" {
		t.Errorf("expected playbook 'install', got %q", opts.Playbook)
	}
}

func TestResolveRunOptsUnknown(t *testing.T) {
	root := testRoot()

	_, err := resolveRunOpts(root, []string{"nonexistent"})
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestResolveRunOptsEmpty(t *testing.T) {
	root := testRoot()

	_, err := resolveRunOpts(root, []string{})
	if err == nil {
		t.Error("expected error for empty args")
	}
}
