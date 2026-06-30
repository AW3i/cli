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

### Refactored TUI Package into Focused Modules

#### Changed

**Improved code organization and maintainability by splitting monolithic exec.go and launcher.go into single-responsibility modules.**

The TUI package grew to ~1,200 lines spread across `exec.go` (756 lines) and `launcher.go` (651 lines), mixing concerns like JSON parsing, error formatting, help UI logic, and screen routing.

**Refactoring split the code into focused modules:**
- **`ansible_events.go`** (~250 lines) — Ansible JSON schema, `parseJSONEvent()`, error/warning formatters (`formatFailureLines()`, `formatWarningLines()`), task identification (`isMetaTask()`, `shortTaskName()`)
- **`help.go`** (~140 lines) — Help UI state and behavior: `helpState` sub-struct, `openHelp()`, `handleHelpKey()`, `helpView()` rendering
- **`layout.go`** (~40 lines) — Pure terminal layout utilities: `dividerLine()`, `wordWrap()`

**Benefits:**
- **Clear separation of concerns** — JSON parsing logic separate from UI rendering; help system in its own module
- **Improved readability** — `exec.go` now 503 lines (-34%), `launcher.go` 497 lines (-26%)
- **Model field reduction** — `launcher.go` dropped from 14 to 10 model fields by grouping help-related fields into `helpState` sub-struct
- **Sub-struct pattern** — `type helpState struct {...}` scales well for adding future screens (settings, filters, etc.) without bloating the main model
- **Testability** — JSON parsing and formatting logic is isolated and independently testable
- **Zero regressions** — All 65 tests pass; binary builds cleanly

**Code changes:**
- New `internal/tui/ansible_events.go` — Ansible JSON schema and event parsing (~250 lines)
- New `internal/tui/help.go` — Interactive help viewer module (~140 lines)
- New `internal/tui/layout.go` — Terminal layout utilities (~40 lines)
- Refactored `internal/tui/exec.go` — removed JSON parsing, reduced from 756 → 503 lines
- Refactored `internal/tui/launcher.go` — removed help logic, reduced from 651 → 497 lines

**Commits:**
- `febcc50` — Refactor TUI into focused modules (ansible_events, help, layout)
- `3f11a9a` — Update CHANGELOG with refactoring summary
- `8549bfc` — Update README.md documentation

---

### CLI Task Display: Real-Time Ansible Task Names in Execution Panel (Superseded by JSON Streaming)

#### Context

This entry describes an intermediate implementation that used custom Python callback plugin output and braille spinner detection. It has since been **completely replaced** by the `ansible.posix.jsonl` callback and structured JSON event streaming (see **Refactored TUI Package** above).

#### Original Problem (Now Solved Differently)

The execution panel spinner was showing stale task names (all tasks arrived at once at process exit, or not at all for long-running operations). The root cause was Python's stdout buffering behavior when connected to a pipe.

**Original approach:**
- Relied on custom `valet-sh.py` callback plugin outputting ANSI-colored spinner characters
- Braille prefix detection (`\xe2\xa0`) to identify first task and unblock BubbleTea
- Task names extracted from spinner output using byte-pattern matching

#### Current Solution (JSON Streaming)

The problem is now solved by using the official `ansible.posix.jsonl` callback plugin:
- Ansible emits structured JSON events directly to stdout (one per line)
- `readTaskCmd()` parses JSON lines containing `v2_playbook_on_task_start` events
- Task names come from the clean JSON `data.task_name` field (no pattern matching needed)
- The gate waits for the first JSON task event instead of braille spinner detection
- Error details (stderr, rc, cmd) are now part of the structured event, displayed directly in the TUI

**See:** `internal/tui/ansible_events.go` for current JSON parsing implementation.

**Old file references (no longer relevant):**
- Old: `cli/internal/ansible/runner.go`
- Current: `internal/ansible/runner.go` (part of unified valet-sh-cli repo)

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

---

## [2.9.19] - Previous Release

(Earlier changes documented in git log)
