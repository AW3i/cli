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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestExecModelInit(t *testing.T) {
	m := NewExecModel("service start php83", "1.0.0", false, nil, nil, 0, 80, 24)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil Cmd (tick + wait)")
	}
}

func TestExecModelDoneOnExecDoneMsg(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, nil, 0, 80, 24)

	rm, _ := m.Update(execDoneMsg{err: nil})

	if !rm.IsDone() {
		t.Error("expected IsDone() true after execDoneMsg")
	}
	if rm.Err() != nil {
		t.Errorf("expected nil error, got %v", rm.Err())
	}
	// Success — should NOT show prompt.
	if rm.awaitingLogPrompt {
		t.Error("should not show log prompt on success")
	}
}

func TestExecModelFailedExecDoneMsg(t *testing.T) {
	m := NewExecModel("service start php83", "1.0.0", false, nil, nil, 0, 80, 24)
	sentinel := errors.New("exit status 1")

	rm, _ := m.Update(execDoneMsg{err: sentinel})

	if !rm.IsDone() {
		t.Error("expected IsDone() true after failed execDoneMsg")
	}
	if rm.Err() != sentinel {
		t.Errorf("expected sentinel error, got %v", rm.Err())
	}
	// Failure — should show prompt.
	if !rm.awaitingLogPrompt {
		t.Error("expected awaitingLogPrompt true after failure")
	}
}

func TestExecModelLogPromptYesOpensViewer(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, nil, 0, 80, 24)

	// Trigger failure.
	m, _ = m.Update(execDoneMsg{err: errors.New("exit status 1")})
	if !m.awaitingLogPrompt {
		t.Fatal("expected awaitingLogPrompt after failure")
	}

	// Press Y.
	m2, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
	if m2.awaitingLogPrompt {
		t.Error("awaitingLogPrompt should be cleared after Y")
	}
	if cmd == nil {
		t.Error("expected loadLogCmd to be returned after Y")
	}

	// Simulate logViewReadyMsg arriving.
	m3, _ := m2.Update(logViewReadyMsg{lines: []string{"line 1", "line 2"}})
	if !m3.logViewOpen {
		t.Error("expected logViewOpen after logViewReadyMsg")
	}
}

func TestExecModelLogPromptEnterOpensViewer(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, nil, 0, 80, 24)
	m, _ = m.Update(execDoneMsg{err: errors.New("exit status 1")})

	// Enter should behave like Y.
	m2, cmd := m.Update(tea.KeyPressMsg{Text: "enter"})
	if m2.awaitingLogPrompt {
		t.Error("awaitingLogPrompt should be cleared after Enter")
	}
	if cmd == nil {
		t.Error("expected loadLogCmd to be returned after Enter")
	}
}

func TestExecModelLogPromptNoQuits(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, nil, 0, 80, 24)
	m, _ = m.Update(execDoneMsg{err: errors.New("exit status 1")})

	// Press N.
	_, cmd := m.Update(tea.KeyPressMsg{Text: "n"})
	if cmd == nil {
		t.Error("expected tea.Quit after n")
	}
}

func TestExecModelLogPromptEscQuits(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, nil, 0, 80, 24)
	m, _ = m.Update(execDoneMsg{err: errors.New("exit status 1")})

	_, cmd := m.Update(tea.KeyPressMsg{Text: "esc"})
	if cmd == nil {
		t.Error("expected tea.Quit after esc")
	}
}

func TestExecModelViewportHeight(t *testing.T) {
	// Normal footer — expect height minus header and 2-line footer.
	h := execViewportHeight(24, false)
	if h != 20 {
		t.Errorf("expected height 20, got %d", h)
	}

	// Prompt footer — extra line for prompt.
	hPrompt := execViewportHeight(24, true)
	if hPrompt != 19 {
		t.Errorf("expected height 19 with prompt, got %d", hPrompt)
	}
}

func TestExecModelLogViewerViewportHeight(t *testing.T) {
	h := logViewerViewportHeight(24)
	if h != 21 {
		t.Errorf("expected height 21, got %d", h)
	}
}

func TestExecModelTaskCounting(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, nil, 10, 80, 24)

	// Simulate task lines appearing in the log.
	m.appendLine("TASK [Gathering Facts]")
	if m.tasksDone != 1 {
		t.Errorf("expected tasksDone=1, got %d", m.tasksDone)
	}

	m.appendLines([]string{
		"some log output",
		"TASK [Start service]",
		"TASK [Enable service]",
	})
	if m.tasksDone != 3 {
		t.Errorf("expected tasksDone=3, got %d", m.tasksDone)
	}
}

func TestTailFile(t *testing.T) {
	dir := t.TempDir()

	// Test reading fewer lines than max.
	path := makeTempLogFile(dir, []string{"line1", "line2", "line3"})
	lines, err := tailFile(path, 10)
	if err != nil {
		t.Fatalf("tailFile failed: %v", err)
	}
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}

	// Test reading from a file larger than max.
	longLines := makeLines(100)
	path = makeTempLogFile(dir, longLines)
	lines, err = tailFile(path, 50)
	if err != nil {
		t.Fatalf("tailFile failed: %v", err)
	}
	if len(lines) != 50 {
		t.Errorf("expected 50 lines (max), got %d", len(lines))
	}
	// Verify we got the last 50 lines in order.
	if lines[0] != "line 51" {
		t.Errorf("expected first line to be 'line 51', got %s", lines[0])
	}
	if lines[49] != "line 100" {
		t.Errorf("expected last line to be 'line 100', got %s", lines[49])
	}
}

func TestTailFileEmpty(t *testing.T) {
	dir := t.TempDir()
	path := makeTempLogFile(dir, nil)

	lines, err := tailFile(path, 10)
	if err != nil {
		t.Fatalf("tailFile failed on empty file: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 lines for empty file, got %d", len(lines))
	}
}

func TestTailFileMissing(t *testing.T) {
	_, err := tailFile("/nonexistent/path/debug.log", 10)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestExecModelProgressBarRendering(t *testing.T) {
	tests := []struct {
		name       string
		tasksDone  int
		totalTasks int
		done       bool
		err        error
		wantBar    bool // whether we expect a bar format vs spinner
	}{
		{"spinner when total unknown", 5, 0, false, nil, false},
		{"bar at 0%", 0, 20, false, nil, true},
		{"bar at 50%", 10, 20, false, nil, true},
		{"bar at 100%", 20, 20, false, nil, true},
		{"bar exceeds total", 25, 20, false, nil, true},
		{"success state", 20, 20, true, nil, false},
		{"failure state", 15, 20, true, errors.New("fail"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewExecModel("install", "1.0.0", false, nil, nil, tc.totalTasks, 80, 24)
			m.tasksDone = tc.tasksDone
			m.done = tc.done
			m.err = tc.err

			view := m.progressBarView()

			if tc.done {
				// Done states show checkmark or X, not a bar.
				if tc.err != nil {
					if !strings.Contains(view, "✘") {
						t.Errorf("expected failure marker in view, got: %s", view)
					}
				} else {
					if !strings.Contains(view, "✔") {
						t.Errorf("expected success marker in view, got: %s", view)
					}
				}
				return
			}

			if tc.wantBar {
				if !strings.Contains(view, "[") || !strings.Contains(view, "]") {
					t.Errorf("expected bar brackets in view, got: %s", view)
				}
				// Check for counter format "x/y".
				counter := fmt.Sprintf("%d/%d", tc.tasksDone, tc.totalTasks)
				if !strings.Contains(view, counter) {
					t.Errorf("expected counter %s in view, got: %s", counter, view)
				}
			} else {
				// Spinner mode — should contain spinner characters.
				if tc.totalTasks == 0 && !strings.Contains(view, "tasks") {
					t.Errorf("expected 'tasks' text in spinner view, got: %s", view)
				}
			}
		})
	}
}

func TestExecModelRenderProgressBarCalculations(t *testing.T) {
	tests := []struct {
		tasksDone  int
		totalTasks int
		wantFilled int // approximate filled cells (allowing for cursor)
	}{
		{0, 20, 0},   // 0% -> 0 filled
		{5, 20, 5},   // 25% -> 5 filled
		{10, 20, 10}, // 50% -> 10 filled
		{20, 20, 20}, // 100% -> 20 filled
		{30, 20, 20}, // exceeds total -> capped at 20
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%d_of_%d", tc.tasksDone, tc.totalTasks), func(t *testing.T) {
			m := NewExecModel("install", "1.0.0", false, nil, nil, tc.totalTasks, 80, 24)
			m.tasksDone = tc.tasksDone

			view := m.progressBarView()

			// Verify bar brackets are present.
			if !strings.Contains(view, "[") || !strings.Contains(view, "]") {
				t.Errorf("expected bar brackets in view, got: %s", view)
			}

			// Verify counter is shown correctly.
			counter := fmt.Sprintf("%d/%d", tc.tasksDone, tc.totalTasks)
			if !strings.Contains(view, counter) {
				t.Errorf("expected counter %s in view, got: %s", counter, view)
			}
		})
	}
}

func TestExecModelRenderProgressBarCursor(t *testing.T) {
	// When incomplete, should show cursor at boundary.
	m := NewExecModel("install", "1.0.0", false, nil, nil, 20, 80, 24)
	m.tasksDone = 10

	view := m.renderProgressBar()
	if !strings.Contains(view, string(progressBarCursor)) {
		t.Errorf("expected cursor '>' in incomplete bar, got: %s", view)
	}

	// When complete, no cursor.
	m.tasksDone = 20
	view = m.renderProgressBar()
	if strings.Contains(view, string(progressBarCursor)) {
		t.Errorf("unexpected cursor in complete bar, got: %s", view)
	}
}

func makeTempLogFile(dir string, lines []string) string {
	path := filepath.Join(dir, "debug.log")
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		panic(fmt.Sprintf("writing temp log: %v", err))
	}
	return path
}

func makeLines(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	return lines
}
