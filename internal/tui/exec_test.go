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
	// Success — should NOT show prompt.
	if rm.awaitingLogPrompt {
		t.Error("should not show log prompt on success")
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
	// Failure — should show prompt.
	if !rm.awaitingLogPrompt {
		t.Error("expected awaitingLogPrompt true after failure")
	}
}

func TestExecModelLogPromptYesOpensViewer(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)

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
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)
	m, _ = m.Update(execDoneMsg{err: errors.New("exit status 1")})

	// Enter should behave like Y.
	m2, cmd := m.Update(tea.KeyPressMsg{Text: "enter"})
	if m2.awaitingLogPrompt {
		t.Error("awaitingLogPrompt should be cleared after enter")
	}
	if cmd == nil {
		t.Error("expected loadLogCmd returned after enter")
	}
}

func TestExecModelLogPromptNoQuitsModel(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)
	m, _ = m.Update(execDoneMsg{err: errors.New("exit status 1")})

	for _, key := range []string{"n", "esc", "q"} {
		_, cmd := m.Update(tea.KeyPressMsg{Text: key})
		if cmd == nil {
			t.Errorf("key %q: expected tea.Quit", key)
		}
	}
}

func TestExecModelLogPromptIgnoresOtherKeys(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)
	m, _ = m.Update(execDoneMsg{err: errors.New("exit status 1")})

	// Random keys should be silently ignored while awaiting prompt.
	for _, key := range []string{"a", "b", "space", "tab", "1", "2"} {
		m2, cmd := m.Update(tea.KeyPressMsg{Text: key})
		if !m2.awaitingLogPrompt {
			t.Errorf("key %q: awaitingLogPrompt should still be true", key)
		}
		if cmd != nil {
			t.Errorf("key %q: expected nil cmd (ignored), got non-nil", key)
		}
	}
}

func TestExecModelQuitOnKeyWhenDoneSuccess(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)
	done, _ := m.Update(execDoneMsg{err: nil})

	_, cmd := done.Update(tea.KeyPressMsg{Text: "enter"})
	if cmd == nil {
		t.Error("expected Quit cmd on keypress after success")
	}
}

func TestExecModelNoQuitOnKeyWhileRunning(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)

	rm, _ := m.Update(tea.KeyPressMsg{Text: "down"})
	if rm.IsDone() {
		t.Error("should not be done after scroll key")
	}
}

func TestExecModelLogViewerQuitKeys(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)
	m, _ = m.Update(execDoneMsg{err: errors.New("fail")})
	m, _ = m.Update(tea.KeyPressMsg{Text: "y"})
	m, _ = m.Update(logViewReadyMsg{lines: []string{"some log"}})

	if !m.logViewOpen {
		t.Fatal("expected logViewOpen")
	}

	for _, key := range []string{"q", "esc", "ctrl+c"} {
		_, cmd := m.Update(tea.KeyPressMsg{Text: key})
		if cmd == nil {
			t.Errorf("key %q in log viewer: expected tea.Quit", key)
		}
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

	content := rm.viewport.GetContent()
	if content != "TASK [install] ok" {
		t.Errorf("expected log line in viewport, got %q", content)
	}
}

func TestExecModelMultipleLogLines(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, 80, 24)

	m2, _ := m.Update(logLineMsg("line 1"))
	m3, _ := m2.Update(logLineMsg("line 2"))

	content := m3.viewport.GetContent()
	if content != "line 1\nline 2" {
		t.Errorf("expected two lines, got %q", content)
	}
}

func TestExecViewportHeight(t *testing.T) {
	calc := func(h, footer int) int {
		v := h - execHeaderHeight - footer
		if v < viewportMinHeight {
			return viewportMinHeight
		}
		return v
	}
	tests := []struct {
		totalHeight    int
		awaitingPrompt bool
		want           int
	}{
		{30, false, calc(30, execFooterHeight)},
		{30, true, calc(30, execFooterHeightPrompt)},
		{6, false, viewportMinHeight}, // 6-2-2=2 < min
		{6, true, viewportMinHeight},  // 6-2-3=1 < min
	}

	for _, tc := range tests {
		got := execViewportHeight(tc.totalHeight, tc.awaitingPrompt)
		if got != tc.want {
			t.Errorf("execViewportHeight(%d, %v) = %d, want %d",
				tc.totalHeight, tc.awaitingPrompt, got, tc.want)
		}
		if got < viewportMinHeight {
			t.Errorf("execViewportHeight(%d, %v) = %d, below minimum",
				tc.totalHeight, tc.awaitingPrompt, got)
		}
	}
}

func TestLogViewerViewportHeight(t *testing.T) {
	got := logViewerViewportHeight(30)
	want := 30 - logViewHeaderHeight - logViewFooterHeight
	if got != want {
		t.Errorf("logViewerViewportHeight(30) = %d, want %d", got, want)
	}
	// Minimum floor.
	small := logViewerViewportHeight(3)
	if small < viewportMinHeight {
		t.Errorf("logViewerViewportHeight(3) = %d, below minimum %d", small, viewportMinHeight)
	}
}

// ---------------------------------------------------------------------------
// tailFile tests
// ---------------------------------------------------------------------------

func TestTailFileEmpty(t *testing.T) {
	path := writeTempLog(t, []string{})
	lines, err := tailFile(path, logViewMaxLines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(lines))
	}
}

func TestTailFileFewLines(t *testing.T) {
	input := []string{"alpha", "beta", "gamma"}
	path := writeTempLog(t, input)

	lines, err := tailFile(path, logViewMaxLines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != len(input) {
		t.Fatalf("expected %d lines, got %d", len(input), len(lines))
	}
	for i, want := range input {
		if lines[i] != want {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], want)
		}
	}
}

func TestTailFileExactMaxLines(t *testing.T) {
	maxLines := 5
	input := makeLines(maxLines)
	path := writeTempLog(t, input)

	lines, err := tailFile(path, maxLines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != maxLines {
		t.Fatalf("expected %d lines, got %d", maxLines, len(lines))
	}
}

func TestTailFileOverMaxLines(t *testing.T) {
	maxLines := 5
	total := 12
	input := makeLines(total)
	path := writeTempLog(t, input)

	lines, err := tailFile(path, maxLines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != maxLines {
		t.Fatalf("expected %d lines, got %d", maxLines, len(lines))
	}
	// Should be the LAST maxLines lines in order.
	for i := 0; i < maxLines; i++ {
		want := input[total-maxLines+i]
		if lines[i] != want {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], want)
		}
	}
}

func TestTailFileOrderPreserved(t *testing.T) {
	// Verify chronological order is maintained for overflow case.
	maxLines := 3
	input := []string{"a", "b", "c", "d", "e"}
	path := writeTempLog(t, input)

	lines, err := tailFile(path, maxLines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"c", "d", "e"}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], w)
		}
	}
}

func TestTailFileMissing(t *testing.T) {
	_, err := tailFile("/nonexistent/path/to/file.log", logViewMaxLines)
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// ---------------------------------------------------------------------------
// Runner tests (resolveRunOpts)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeTempLog(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.log")
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp log: %v", err)
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
