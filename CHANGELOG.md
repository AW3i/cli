# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Interactive Help System with `?` Keybinding

#### Added

**New `?` help keybinding in TUI launcher for discovering command usage without interrupting workflow.**

The interactive TUI launcher now includes a dedicated help viewer accessible via the `?` key. This allows users to:
- Explore command usage, description, and full help text from playbook annotations
- Scroll through help content with vi-style (j/k) or arrow key navigation
- Seamlessly transition from reading help to running the command with Enter
- Close help and return to the command list with q/esc/? (toggle behavior)

**Help View Features:**
- Accessible from both the command list and inline argument box via `?` key
- Display command usage, short description, and full help text
- Fixed height (8 lines) prevents viewport jumping and keeps UI stable
- Word-wrapped to terminal width with smart padding
- Scrollable with `↑/↓`, `j/k`, `ctrl+u/d` keybindings
- Enter opens the inline box for the current command (quick path to execution)
- Toggle on/off with `?` (matches pager conventions like `less`, `man`)

**UI/UX Improvements:**
- Help bar now displays all available keybindings:
  - List screen: `h/l` navigate · `/` filter · `?` help · `↵` select · `esc` back · `q` quit
  - Inline box: `?` help · `↵` run · `ctrl+d/u` scroll docs · `esc` back
  - Help view: `↑/↓` scroll · `j/k` vim scroll · `q/esc` close · `?` toggle · `enter` run
- Fixed misleading "type to search" hint → now shows `/ filter` (accurate bubbles/list key)
- Added screen state to state machine: `screenHelp` (read-only, scrollable help viewer)

**Bug Fixes:**
- Fixed last item display in narrow terminals (add -1 safety margin for ambiguous-width Unicode chars like ▶ in Nerd Fonts)
- Fixed terminal viewport jumping when help opens (use fixed height instead of dynamic)
- Fixed filter keybinding accuracy in help bar (show / not "type to search")

**Code Changes:**
- `internal/tui/launcher.go` — added `screenHelp` state, `openHelp()`, `handleHelpKey()`, `helpView()`, `helpViewMaxLines` constant (+140 lines)
- `internal/tui/launcher_test.go` — added 8 new test cases for help view open/close/scroll/inline integration (+157 lines)
- `internal/tui/list.go` — updated help bar keybinding hints, added -1 safety margin for ambiguous Unicode width (+10 lines)

**Tests:** All 65 tests pass ✓

---

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
