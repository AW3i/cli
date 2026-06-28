# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### CLI Task Display: Real-Time Ansible Task Names in Execution Panel

#### Fixed

**Task names now display in real time as Ansible moves from task to task.**

The execution panel spinner was showing stale task names (all tasks arrived at once at process exit, or not at all for long-running operations). The root cause was Python's stdout buffering behavior when connected to a pipe.

**The Problem:**

- Ansible's callback plugin (`/usr/local/valet-sh/valet-sh/plugins/callback/valet-sh.py`) writes task names using `print(..., end="\r")` to stdout
- When stdout is a pipe (not a TTY), Python uses **full buffering** by default — the 8 KB buffer holds task name writes
- Without explicit `sys.stdout.flush()` in the callback plugin, task names sit in the buffer until:
  - The buffer fills (rare, after ~100 tasks)
  - The process exits (common case — all task names arrive at once at the very end)
- Result: task display showed nothing for minutes, then suddenly all tasks at once, or was completely stale for long operations

**The Solution:**

Set `PYTHONUNBUFFERED=1` in the Ansible subprocess environment (`cli/internal/ansible/runner.go:248`). This forces Python to use **line buffering** for stdout when connected to a pipe, ensuring each `print()` call goes to the pipe immediately.

**Effect:**
- Each task arrival now generates a separate read from the stdout pipe in real time
- The Go goroutine `readTaskCmd()` receives `"\x1b[2K\r⠙ taskname\r"` (flush + spinner + task) immediately
- Progress bar updates instantly as Ansible moves to each new task
- Display naturally reflects what Ansible is currently executing

**Code changes:**
- `cli/internal/ansible/runner.go` — added `"PYTHONUNBUFFERED=1"` to subprocess environment (1-line fix)
- `cli/internal/tui/exec.go` — defensive improvements to task name parsing:
  - `readTaskCmd()` internal loop: only return on IO error/EOF, not on empty parse results (avoids spurious EOF from flush-only reads)
  - `parseAnsibleTaskLine()` added spinner rune validation: only accept lines starting with known braille spinner characters (`⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`), preventing play-start lines from being misidentified
- `cli/internal/tui/exec_test.go` — expanded test coverage with 7 cases for `parseAnsibleTaskLine` and flush-only line handling

**Commits:**
- `360c34c` — Stream task names from ansible stdout pipe (had buffering issue)
- `6af1c41` — Previous log-file-based approach (was working)
- Latest — `PYTHONUNBUFFERED=1` fix + defensive parsing improvements (**current**)

**Testing:**
- All unit tests pass
- Real-world verification pending: run `valet service start` or `valet init-instance` to confirm task display updates correctly

See [docs/architecture.md - CLI Task Display: Real-Time Streaming](docs/architecture.md#cli-task-display-real-time-streaming) for detailed design documentation.

---

**Log Viewer Prompt: Skip Intermediate State on Y/Y** ([`49c1675`](https://github.com/valet-sh/valet-sh/commit/49c1675))

Fixed the "View full log?" prompt interaction to open log viewer with a single keypress.

**Problem:** When Ansible failed:
1. User presses any key → prompt appears "View full log? [Y/n]"
2. User presses Y → log viewer opens
- Result: required two separate keypresses to view the log

**Solution:** When the first error-state keypress is `y` or `Y`, immediately open the log viewer without showing the intermediate prompt. Other keys still show the prompt as expected.

**Code changes:**
- `handleKey()` in error state — added check: if key is `y/Y`, call `loadLogCmd()` directly; otherwise show prompt

**User experience:**
- Pressing `y` directly on error: log opens immediately (one keystroke)
- Pressing other key on error: prompt shows, user can then press `y/Y` or `n` (two stage process)
- Consistent with natural user expectation: "I want to see the log" → single action

---

### Technical Details

**Files modified:**
- `cli/internal/ansible/runner.go` — added `PYTHONUNBUFFERED=1` to subprocess environment
- `cli/internal/tui/exec.go` — improved `readTaskCmd` loop and `parseAnsibleTaskLine` validation
- `cli/internal/tui/exec_test.go` — expanded test coverage for task parsing

**Testing:**
- Build: successful, no compilation errors
- All unit tests pass
- Changes are in `cli` subdirectory with its own `go.mod`

---

## [2.9.19] - Previous Release

(Earlier changes documented in git log)
