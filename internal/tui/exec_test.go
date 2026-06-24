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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestExecModelInit(t *testing.T) {
	m := NewExecModel("service start php83", "1.0.0", false, nil, nil, nil, 0, 80, 24)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil Cmd (tick + wait)")
	}
}

func TestExecModelDoneOnExecDoneMsg(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, nil, nil, 0, 80, 24)

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
	m := NewExecModel("service start php83", "1.0.0", false, nil, nil, nil, 0, 80, 24)
	sentinel := errors.New("exit status 1")

	rm, _ := m.Update(execDoneMsg{err: sentinel})

	if !rm.IsDone() {
		t.Error("expected IsDone() true after failed execDoneMsg")
	}
	if rm.Err() != sentinel {
		t.Errorf("expected sentinel error, got %v", rm.Err())
	}
	// Prompt is NOT shown immediately — it appears on first keypress.
	if rm.awaitingLogPrompt {
		t.Error("awaitingLogPrompt should not be set by execDoneMsg (only on keypress)")
	}
}

func TestExecModelLogPromptYesOpensViewer(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, nil, nil, 0, 80, 24)

	// Trigger failure.
	m, _ = m.Update(execDoneMsg{err: errors.New("exit status 1")})

	// Press Y directly — skips prompt and opens log immediately.
	m2, cmd := m.Update(tea.KeyPressMsg{Text: "y"})
	if m2.awaitingLogPrompt {
		t.Error("awaitingLogPrompt should not be set when Y pressed directly")
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
	m := NewExecModel("install", "1.0.0", false, nil, nil, nil, 0, 80, 24)
	m, _ = m.Update(execDoneMsg{err: errors.New("exit status 1")})

	// Any other key triggers the prompt; then Enter confirms.
	m, _ = m.Update(tea.KeyPressMsg{Text: "x"}) // show prompt
	if !m.awaitingLogPrompt {
		t.Fatal("expected awaitingLogPrompt after non-y keypress on failure")
	}
	m2, cmd := m.Update(tea.KeyPressMsg{Text: "enter"})
	if m2.awaitingLogPrompt {
		t.Error("awaitingLogPrompt should be cleared after Enter")
	}
	if cmd == nil {
		t.Error("expected loadLogCmd to be returned after Enter")
	}
}

func TestExecModelLogPromptNoQuits(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, nil, nil, 0, 80, 24)
	m, _ = m.Update(execDoneMsg{err: errors.New("exit status 1")})

	// Show prompt with any neutral key, then press N.
	m, _ = m.Update(tea.KeyPressMsg{Text: "x"}) // show prompt
	if !m.awaitingLogPrompt {
		t.Fatal("expected awaitingLogPrompt after neutral keypress")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Text: "n"})
	if cmd == nil {
		t.Error("expected tea.Quit after n in prompt")
	}
}

func TestExecModelLogPromptEscQuits(t *testing.T) {
	m := NewExecModel("install", "1.0.0", false, nil, nil, nil, 0, 80, 24)
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
	m := NewExecModel("install", "1.0.0", false, nil, nil, nil, 10, 80, 24)

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
		name        string
		tasksDone   int
		totalTasks  int
		done        bool
		err         error
		currentTask string
		expectedIn  string // What we expect to find in the view (after shortening)
	}{
		{"spinner with no task", 5, 0, false, nil, "", "running..."},
		{"spinner with task", 10, 20, false, nil, "my : task", "task"}, // "my : task" gets shortened to "task"
		{"success state", 20, 20, true, nil, "", "✔"},
		{"failure state", 15, 20, true, errors.New("fail"), "", "✘"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := NewExecModel("install", "1.0.0", false, nil, nil, nil, tc.totalTasks, 80, 24)
			m.tasksDone = tc.tasksDone
			m.done = tc.done
			m.err = tc.err
			m.currentTask = tc.currentTask

			view := m.progressBarView()

			if !strings.Contains(view, tc.expectedIn) {
				t.Errorf("expected %q in view, got: %s", tc.expectedIn, view)
			}
		})
	}
}

func TestExecModelRenderProgressBarCalculations(t *testing.T) {
	// The progress bar shows spinner + task name. Verify task name appears correctly.
	// Note: task names are shortened to remove role prefix.
	tasks := []struct {
		tasksDone  int
		totalTasks int
		task       string
		expectedIn string // What should appear in the view (shortened)
	}{
		{0, 20, "first : task", "task"},   // Shortened from "first : task"
		{10, 20, "middle : task", "task"}, // Shortened from "middle : task"
		{20, 20, "last : task", "task"},   // Shortened from "last : task"
	}

	for _, tc := range tasks {
		t.Run(fmt.Sprintf("%d_of_%d", tc.tasksDone, tc.totalTasks), func(t *testing.T) {
			m := NewExecModel("install", "1.0.0", false, nil, nil, nil, tc.totalTasks, 80, 24)
			m.tasksDone = tc.tasksDone
			m.currentTask = tc.task

			view := m.progressBarView()

			if !strings.Contains(view, tc.expectedIn) {
				t.Errorf("expected %q in progress bar view, got: %s", tc.expectedIn, view)
			}
		})
	}
}

func TestExecModelProgressBarView(t *testing.T) {
	// While running: should show spinner + task name (shortened).
	m := NewExecModel("install", "1.0.0", false, nil, nil, nil, 20, 80, 24)
	m.tasksDone = 10
	m.currentTask = "some : task name"

	view := m.progressBarView()
	// Task name gets shortened to just "task name"
	if !strings.Contains(view, "task name") {
		t.Errorf("expected shortened task name in progress bar, got: %s", view)
	}

	// When done with success: should show checkmark.
	m.done = true
	m.tasksDone = 20
	view = m.progressBarView()
	if !strings.Contains(view, "✔") {
		t.Errorf("expected checkmark in completed bar, got: %s", view)
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

func TestParseAnsibleTaskLine(t *testing.T) {
	// Simulate what the callback plugin writes to stdout per task:
	// \x1b[2K\r\033[0;32m⠙\033[0;0m taskname\r
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "typical callback output",
			input: "\x1b[2K\r\033[0;32m⠙\033[0;0m ensure rabbitmq is started\r",
			want:  "ensure rabbitmq is started",
		},
		{
			name:  "different spinner frame",
			input: "\x1b[2K\r\033[0;32m⠸\033[0;0m shared-variables : set 'current_os' var\r",
			want:  "shared-variables : set 'current_os' var",
		},
		{
			name:  "multiple segments — last wins",
			input: "\x1b[2K\r\033[0;32m⠋\033[0;0m first task\r\x1b[2K\r\033[0;32m⠙\033[0;0m second task\r",
			want:  "second task",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			// The callback writes \x1b[2K\r BEFORE the spinner+taskname line.
			// A Read() call that returns only the FLUSH prefix must not extract
			// any task name (previously this caused readTaskCmd to stop early).
			name:  "flush-only line returns empty",
			input: "\x1b[2K\r",
			want:  "",
		},
		{
			// The callback writes a play-start line with \n (not \r):
			//   print(BLUE + "▶ " + BOLD + name + RESET + CURSOR_HIDE, end="\n")
			// This must not be mistaken for a task name (previously the
			// play-start marker ▶ was stripped as if it were a spinner, yielding
			// the play name instead of a task name).
			name:  "play-start line returns empty",
			input: "\x1b[1;34m▶ \x1b[;1mvalet.sh\x1b[0;0m\x1b[?25l\n",
			want:  "",
		},
		{
			// A Read() that captures the play-start line + FLUSH + a real task
			// must still return the task name (not the play name).
			name:  "play-start mixed with task line — task name wins",
			input: "\x1b[1;34m▶ \x1b[;1mvalet.sh\x1b[0;0m\x1b[?25l\n\x1b[2K\r\x1b[0;32m⠙\x1b[0;0m Gathering Facts\r",
			want:  "Gathering Facts",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAnsibleTaskLine([]byte(tc.input))
			if got != tc.want {
				t.Errorf("parseAnsibleTaskLine(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseLogTaskName(t *testing.T) {
	// Test parsing task names from debug.log format:
	// "TASK [role-name : task-name] ****..."
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "typical task line",
			input: "TASK [sudo-permission-check : Reset sudo session] ******************************",
			want:  "sudo-permission-check : Reset sudo session",
		},
		{
			name:  "task line with short role",
			input: "TASK [shared-variables : set 'current_user', 'current_group' and 'current_home'] ***",
			want:  "shared-variables : set 'current_user', 'current_group' and 'current_home'",
		},
		{
			name:  "task with no role prefix",
			input: "TASK [Gathering Facts] ****",
			want:  "Gathering Facts",
		},
		{
			name:  "empty brackets",
			input: "TASK [] ****",
			want:  "",
		},
		{
			name:  "no opening bracket",
			input: "TASK role-name : task-name] ****",
			want:  "",
		},
		{
			name:  "no closing bracket",
			input: "TASK [role-name : task-name ****",
			want:  "",
		},
		{
			name:  "not a task line (but still has brackets) — parseLogTaskName extracts anyway",
			input: "PLAY [some play] ****",
			want:  "some play",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLogTaskName(tc.input)
			if got != tc.want {
				t.Errorf("parseLogTaskName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestReadTaskCmdSkipsFlushLines(t *testing.T) {
	// Simulate a pipe that sends: FLUSH-only data, then a real task line.
	// readTaskCmd must skip the FLUSH and return the task name, not stop early.
	flush := "\x1b[2K\r"
	taskLine := "\x1b[2K\r\x1b[0;32m⠙\x1b[0;0m my : task name\r"

	pr, pw := io.Pipe()

	// Writer goroutine: send FLUSH then task line.
	go func() {
		pw.Write([]byte(flush))
		// small delay to ensure two separate Read() calls are possible
		pw.Write([]byte(taskLine))
		pw.Close()
	}()

	cmd := readTaskCmd(pr)
	msg := cmd()

	taskMsg, ok := msg.(ansibleTaskMsg)
	if !ok {
		t.Fatalf("expected ansibleTaskMsg, got %T", msg)
	}
	if string(taskMsg) != "my : task name" {
		t.Errorf("expected %q, got %q", "my : task name", string(taskMsg))
	}
}

func TestIsMetaTask(t *testing.T) {
	// Test meta-task detection
	tests := []struct {
		name       string
		input      string
		isMetaTask bool
	}{
		{
			name:       "include_tasks is meta",
			input:      "valet-init-instance : include_tasks",
			isMetaTask: true,
		},
		{
			name:       "import_tasks is meta",
			input:      "valet-init-instance : import_tasks",
			isMetaTask: true,
		},
		{
			name:       "include_role is meta",
			input:      "some-role : include_role",
			isMetaTask: true,
		},
		{
			name:       "import_role is meta",
			input:      "some-role : import_role",
			isMetaTask: true,
		},
		{
			name:       "include_tasks without role is meta",
			input:      "include_tasks",
			isMetaTask: true,
		},
		{
			name:       "real task is not meta",
			input:      "valet-init-instance : ensure elasticsearch is started",
			isMetaTask: false,
		},
		{
			name:       "task with pipe separator is not meta",
			input:      "valet-init-instance : workflows » magento2 » services » php | ensure php8.3 is started",
			isMetaTask: false,
		},
		{
			name:       "Gathering Facts is not meta",
			input:      "Gathering Facts",
			isMetaTask: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isMetaTask(tc.input)
			if got != tc.isMetaTask {
				t.Errorf("isMetaTask(%q) = %v, want %v", tc.input, got, tc.isMetaTask)
			}
		})
	}
}

func TestShortTaskName(t *testing.T) {
	// Test task name shortening
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "role-qualified with pipe separator",
			input: "valet-init-instance : workflows » magento2 » services » php | ensure php8.3 is started",
			want:  "ensure php8.3 is started",
		},
		{
			name:  "role-qualified without pipe",
			input: "shared-variables : set 'current_os' var",
			want:  "set 'current_os' var",
		},
		{
			name:  "Gathering Facts",
			input: "Gathering Facts",
			want:  "Gathering Facts",
		},
		{
			name:  "include_tasks with role",
			input: "valet-init-instance : include_tasks",
			want:  "include_tasks",
		},
		{
			name:  "long workflow task",
			input: "valet-init-instance : workflows » magento2 » services » elasticsearch | wait for elasticsearch to be reachable",
			want:  "wait for elasticsearch to be reachable",
		},
		{
			name:  "empty after pipe (keep full)",
			input: "valet-init-instance : some task | ",
			want:  "",
		},
		{
			name:  "multiple pipes (last one wins)",
			input: "valet-init-instance : task | part 1 | part 2",
			want:  "part 2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shortTaskName(tc.input)
			if got != tc.want {
				t.Errorf("shortTaskName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestAppendLinesSkipsMetaTasks(t *testing.T) {
	// Test that include_tasks doesn't update currentTask if a real task came before
	m := NewExecModel("install", "1.0.0", false, nil, nil, nil, 10, 80, 24)

	// First, set a real task
	m.appendLine("TASK [some-role : ensure service is started] ****")
	realTask := m.currentTask
	if realTask == "" {
		t.Fatal("expected currentTask to be set to real task")
	}

	// Then try to append include_tasks
	m.appendLine("TASK [some-role : include_tasks] ****")

	// currentTask should NOT change (still the real task)
	if m.currentTask != realTask {
		t.Errorf("currentTask changed from %q to %q when include_tasks was processed", realTask, m.currentTask)
	}

	// Batch append with real task AFTER include_tasks
	m.appendLines([]string{
		"some output",
		"TASK [some-role : include_tasks] ****",
		"TASK [workflow : real work | do important thing] ****",
	})

	// currentTask should be the real task after include_tasks
	if !strings.Contains(m.currentTask, "do important thing") {
		t.Errorf("expected currentTask to be 'do important thing' task, got %q", m.currentTask)
	}
}
