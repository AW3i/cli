# valet-sh CLI

Go-based CLI that orchestrates Ansible playbooks for managing local development
environments for Magento, PHP, and other projects. When invoked with no
arguments it launches an interactive Bubble Tea TUI; with arguments it shows a
live execution panel and delegates to Ansible.

---

## Architecture

### Entry Point

`cmd/valet/main.go` — Cobra root command. Commands are auto-discovered from
`playbooks/*.yml` header annotations at startup. On every invocation:

1. Periodic update check (weekly, skipped for `--help`/`--version`)
2. No args → TUI launcher (`tui.Run`) → on command selection → `tui.RunWithPanel`
3. Args on TTY → execution panel (`tui.RunWithPanel`)
4. Args on non-TTY (CI/pipe) → `syscall.Exec` into `ansible-playbook` directly

### Package Structure

```
├── cmd/valet/
│   └── main.go              Entry point, routing between TUI and Ansible
├── internal/
│   ├── ansible/             Subprocess runner (Run via syscall.Exec,
│   │                        RunSubprocess for TUI panel)
│   ├── commands/
│   │   ├── discover.go      Auto-discovers cobra commands from playbooks/*.yml
│   │   ├── hooks.go         PreRunE validation hooks (.valet-sh.yml checks)
│   │   ├── help.go          Colored help formatter
│   │   └── helpers.go       requireArgs / requireMinArgs validators
│   ├── config/              .valet-sh.yml parser + global config reader
│   ├── platform/            OS/arch detection, ansible-playbook path
│   ├── tui/                 Bubble Tea TUI (launcher, exec panel, log viewer)
│   └── updater/             Weekly GitHub Releases update check
└── .golangci.yml            Linting configuration
```

### Command Auto-Discovery

Commands are **not** defined in Go. `commands.Discover(repoDir)` scans
`playbooks/*.yml` at startup and builds cobra commands from header annotations:

```yaml
# @command:     "service"
# @description: "start/stop or enable/disable a service"
# @usage:       "valet.sh service <start|stop|restart|enable|disable|default> svc"
# @help:
# start: start a valet-sh service
# valet.sh service start mysql80
```

| Annotation | cobra field | Notes |
|---|---|---|
| `@command` | `cmd.Annotations["playbook"]` | Canonical playbook name, e.g. `project:env` |
| `@description` | `cmd.Short` | Shown in list and TUI launcher |
| `@usage` | `cmd.Use` | `valet.sh ` prefix is stripped |
| `@help` | `cmd.Long` | Multi-line block until `---` separator |

Playbooks with a colon in `@command` (e.g. `project:env`) are automatically
grouped: `project` becomes a parent cobra command with `env` as a subcommand.

**Adding a new command = adding a new `playbooks/<name>.yml` with annotations.**
No Go code needs to be written or modified.

### TUI Package (`internal/tui/`)

| File | Responsibility |
|---|---|
| `launcher.go` | Navigation + help system: command bar, inline box, help viewer, quits with `Result.Args` on Enter |
| `list.go` | `CommandItem` (list.Item), custom delegate, arg parsing from cobra `Use`, keybinding hints |
| `inline.go` | Inline command box (arg input + docs display, ctrl+d/u scrolling) |
| `exec.go` | `ExecModel`: live execution panel, JSON event streaming, log viewer on failure |
| `runner.go` | `RunWithPanel()` entry point, `waitForFirstJSONTask()` gate, `standaloneExecModel` |
| `ansible_events.go` | Ansible JSON schema, event parsing, error/warning formatting, task name shortening |
| `help.go` | Interactive help viewer: `helpState` sub-struct, open/close/scroll behavior |
| `layout.go` | Terminal layout utilities: `dividerLine()`, `wordWrap()` |
| `styles.go` | Lip Gloss styles matching the Ansible callback colour palette |

**Screen state machine:**

```
screenList    →  (enter on leaf)  →  screenInline
             →  (? on leaf)       →  screenHelp
screenInline  →  (enter)          →  [launcher quits, Result.Args set]
             →  (?)              →  screenHelp
screenHelp    →  (enter)          →  screenInline (run command)
             →  (q/esc/?)        →  screenList (close help)
screenExec    →  (done, failure)  →  logViewOpen = true
```

**Interactive help system:**

The TUI launcher includes an integrated help viewer accessible via the `?` key:

| Screen | Key | Action |
|---|---|---|
| List | `?` | Open read-only help view for selected command |
| Inline box | `?` | Close box and open help view for selected command |
| Help view | `↑/↓` or `j/k` | Scroll help text up/down |
| Help view | `enter` | Close help and open inline box (to run command) |
| Help view | `q`/`esc`/`?` | Close help and return to list |

Help text is sourced directly from the playbook's `@help:` annotation and includes:
- Command usage (from `@usage:`)
- Short description (from `@description:`)
- Full multi-line help block (from `@help:`)

The help view uses a fixed height (8 lines) to prevent viewport jumping when opened.

**Two execution paths:**

| Path | Used when | Mechanism |
|---|---|---|
| `ansible.Run()` | Non-TTY / direct cobra dispatch | `syscall.Exec` — replaces current process |
| `ansible.RunSubprocess()` | TUI panel | `exec.Cmd.Start()` — Go stays alive for JSON event streaming and TUI rendering |

---

### Password Handling & TUI Startup Gate

Ansible playbooks that require privilege escalation declare `vars_prompt` for
`ansible_become_pass`. Go **never touches the password** — Ansible handles it
natively via its own prompt mechanism.

#### How it works

`RunSubprocess` passes `cmd.Stdin = os.Stdin` to the Ansible subprocess.
When the playbook starts, Ansible's `vars_prompt` prompts for the password on
the raw terminal. Go gates BubbleTea startup on `waitForFirstJSONTask()`.

#### The startup gate (`waitForFirstJSONTask`)

The Ansible playbook uses the official `ansible.posix.jsonl` callback plugin, which
emits structured JSON events to stdout in this order:

```
1. v2_playbook_on_play_start    — fires BEFORE vars_prompt
2.                               — vars_prompt fires here; Ansible reads password from stdin
3. v2_playbook_on_task_start    — first task starts (first JSON event containing task info)
```

`waitForFirstJSONTask()` reads the stdout pipe line-by-line, buffering all JSON events,
until it detects an event with `"event": "v2_playbook_on_task_start"`. This JSON 
structure only appears after the first real task starts (step 3), guaranteeing that 
`vars_prompt` (step 2) has completed and stdin is free before BubbleTea takes over.

**JSON event detection:**

The gate scans for the literal JSON substring `"v2_playbook_on_task_start"` within each
line. This is unambiguous:
- `v2_playbook_on_play_start` events come before the password prompt
- `v2_playbook_on_task_start` is the first task-level event, guaranteed after password entry
- If parsing fails, the gate waits for the next line (no false positives)

All bytes consumed while waiting are returned via `io.MultiReader` so the exec
panel receives the complete, unmodified stdout stream.

**If Ansible exits before any task** (e.g. syntax error, missing playbook),
the stdout pipe closes. `waitForFirstJSONTask()` returns the buffered bytes and
`runExecPanel()` starts immediately, receiving `execDoneMsg` with the error.

#### TUI launcher path

The launcher is navigation-only. On Enter, it stores `selectedArgs` and returns
`tea.Quit`. `tui.Run()` returns `Result{Args: selectedArgs}`. `main.go` then
calls `RunWithPanel(root, result.Args, Version)` on the clean terminal after
BubbleTea has torn down — so `vars_prompt` fires on the raw terminal, then the
exec panel BubbleTea starts after `waitForFirstJSONTask()` unblocks.

---

### Key Design Decisions

1. **Go orchestrates, Ansible executes** — the Go CLI adds UX (auto-discovery,
   typed validation, help, TUI) but all heavy lifting remains in Ansible.

2. **`syscall.Exec` for non-interactive** — signals (Ctrl-C) flow directly to
   `ansible-playbook`, Go process vanishes from the process table. Used when
   stdout is not a TTY.

3. **`RunSubprocess` for TUI** — Go stays alive to stream JSON events from Ansible
   and render the execution panel with real-time task updates and structured error details.

4. **Ansible owns the password** — `ansible_become_pass` is never collected,
   stored, or passed by Go. `vars_prompt` in the playbook handles it natively
   via the raw terminal. `waitForFirstJSONTask()` gates BubbleTea startup on the
   first `v2_playbook_on_task_start` JSON event, ensuring stdin is free during the
   password prompt.

5. **No `//nolint` comments** — use `.golangci.yml` exclusions with an
   explanation.

6. **Bubble Tea value receivers** — required by the Elm architecture.
   `hugeParam` warnings are excluded in `.golangci.yml` for all `internal/tui/`
   files. Do not convert to pointer receivers.

---

## Development

### Prerequisites

- Go 1.22+
- `golangci-lint` v1.64.8 (auto-installed by `make lint`)

### Build

```bash
make build          # Build for current OS/arch → dist/valet
make build-all      # Cross-compile for all 4 platforms → dist/
make install        # Build + copy to /usr/local/valet-sh/bin/valet
```

### Running against a local valet-sh checkout

By default the binary reads playbooks and `ansible.cfg` from the **installed**
path `/usr/local/valet-sh/valet-sh`. During development you want to point it at
your local `valet-sh` checkout instead — without overwriting the production
install.

Set `VALET_REPO_DIR` before running the dev binary:

```bash
export VALET_REPO_DIR=/path/to/valet-sh

# Now the binary uses your local playbooks and ansible.cfg
./dist/valet service list
./dist/valet --help
```

When `VALET_REPO_DIR` is set the binary prints a notice at the end of every
command so you always know which repo is active:

```
[dev] repo: /path/to/valet-sh
```

Add the export to your shell profile (`~/.bashrc` / `~/.zshrc`) to make it
permanent for your development session, or prefix individual commands:

```bash
VALET_REPO_DIR=/path/to/valet-sh ./dist/valet service list
```

### Test

```bash
make test           # go test -race ./...
make test-coverage  # Run with coverage report printed to stdout
```

### Lint

```bash
make lint           # golangci-lint run (auto-installs if missing)
make lint-ci        # go mod download + golangci-lint (mirrors CI exactly)
make quality        # fmt-check + vet + mod-verify + lint
```

### Before Every Push

```bash
make lint && go test -race ./... && go build ./...
```

All three must pass. Commit only after they do.

---

## Configuration

### `.valet-sh.yml` Format

```yaml
hub:
  host: "git.example.com"
  port: 22
  path: "/data"
services:
  php:
    version: 8.1
  mariadb:
    version: 10.6
    database: magento
  elasticsearch:
    version: 7
    plugins: ["analysis-icu"]
instance:
  key: "myproject"     # hostname: myproject.test
  type: "magento2"     # bootstrap workflow
  path: "src"          # docroot
```

### Supported instance types

`magento2`, `magento1`, `neos`, `aem`, `orocrm`

---

## Commands

Commands are auto-discovered from `playbooks/*.yml` header annotations. To add
a command, add a new playbook with the required annotations — no Go code needed.

Commands that require a valid `.valet-sh.yml` in the current directory (`link`,
`init-instance`) have a `PreRunE` hook registered by `commands.ApplyHooks()`.

All commands follow the same runtime pattern:

1. `commands.Discover()` builds the cobra command from playbook annotations
2. Cobra parses args; `hooks.go` validates `.valet-sh.yml` if needed
3. `resolveRunOpts()` builds `ansible.RunOpts` (reads `cmd.Annotations["playbook"]`)
4. On TTY: `ansible.RunSubprocess()` + `waitForFirstTask()` + exec panel
5. On non-TTY: `ansible.Run()` → `syscall.Exec` into `ansible-playbook`

---

## Release Process

1. Tag with `v*` pattern: `git tag v2.10.0 && git push origin v2.10.0`
2. GitHub Actions builds 4 binaries:
   - `valet-linux-amd64`, `valet-linux-arm64`
   - `valet-darwin-amd64`, `valet-darwin-arm64`
3. Binaries + `checksums.txt` attached to GitHub Release
4. `valet-sh/installer` downloads the appropriate binary on `setup`/`update`

---

## golangci-lint Exclusions

| Path | Linter | Reason |
|---|---|---|
| `internal/commands/help.go` | `errcheck` | `fmt.Fprintln` to stdout is best-effort |
| `internal/commands/helpers.go` | `errcheck` | `cmd.Help()` stdout errors are non-critical |
| `internal/updater/check.go` | `errcheck` | File close and HTTP body close are best-effort |
| `internal/tui/` | `gocritic/hugeParam` | Bubble Tea requires value receivers — intentional by design |
| `internal/tui/runner.go` | `errcheck` | Best-effort workdir lookup |
| All | `gosec/G204` | `syscall.Exec` with trusted argv is intentional |

---

## Security Notes

- `syscall.Exec` argv is constructed from platform constants + cobra-parsed args
- `go.sum` pins exact SHA-256 hashes for all Go module dependencies
- Ansible's `ansible_become_pass` is never collected or stored by Go;
  `vars_prompt` in the playbook prompts natively on the raw terminal
- `waitForFirstJSONTask()` gates BubbleTea on JSON event detection,
  ensuring stdin is free before BubbleTea takes ownership

---

## License

Apache 2.0 — see repository root for full license text.
Copyright 2025 TechDivision GmbH
