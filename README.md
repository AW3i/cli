# valet-sh CLI

Go-based CLI that orchestrates Ansible playbooks for managing local development
environments for Magento, PHP, and other projects. When invoked with no
arguments it launches an interactive Bubble Tea TUI; with arguments it shows a
live execution panel and delegates to Ansible.

---

## Architecture

### Entry Point

`cmd/valet/main.go` — Cobra root command with 17 subcommands. On every
invocation:

1. Periodic update check (weekly, skipped for `--help`/`--version`)
2. No args → TUI launcher (`tui.Run`)
3. Args on TTY → execution panel (`tui.RunWithPanel`)
4. Args on non-TTY (CI/pipe) → `syscall.Exec` into `ansible-playbook` directly

### Package Structure

```
cli/
├── cmd/valet/
│   └── main.go              Entry point, routing between TUI and Ansible
├── internal/
│   ├── ansible/             Subprocess runner (Run via syscall.Exec,
│   │                        RunSubprocess for TUI panel)
│   ├── commands/            Cobra command implementations (one file each)
│   ├── config/              .valet-sh.yml parser + global config reader
│   ├── platform/            OS/arch detection, ansible-playbook path,
│   │                        service name normalisation
│   ├── tui/                 Bubble Tea TUI (launcher, exec panel, log viewer)
│   └── updater/             Weekly GitHub Releases update check
└── .golangci.yml            Linting configuration
```

### TUI Package (`internal/tui/`)

| File | Responsibility |
|---|---|
| `launcher.go` | Root Bubble Tea model: navigation stack, screen state machine |
| `list.go` | `CommandItem` (list.Item), custom delegate, arg parsing from cobra `Use` |
| `args.go` | Argument input pane (`bubbles/textinput` per arg) |
| `exec.go` | `ExecModel`: live log panel, `debug.log` tail, log viewer on failure |
| `runner.go` | `RunWithPanel()` entry point, `standaloneExecModel` wrapper |
| `styles.go` | Lip Gloss styles matching the Ansible callback colour palette |

**Screen state machine:**

```
screenList  →  (enter on leaf with args)  →  screenArgs
screenList  →  (enter on leaf, no args)   →  screenExec
screenArgs  →  (enter, all required set)  →  screenExec
screenExec  →  (done, failure)            →  logViewOpen = true
```

**Two execution paths:**

| Path | Used when | Mechanism |
|---|---|---|
| `ansible.Run()` | Non-TTY / direct cobra dispatch | `syscall.Exec` — replaces current process |
| `ansible.RunSubprocess()` | TUI panel | `exec.Cmd.Start()` — Go stays alive for log tailing |

### Key Design Decisions

1. **Go orchestrates, Ansible executes** — the Go CLI adds UX (typed validation,
   help, TUI) but all heavy lifting remains in Ansible. Individual commands will
   gradually be reimplemented in Go where it makes sense.

2. **`syscall.Exec` for non-interactive** — signals (Ctrl-C) flow directly to
   `ansible-playbook`, Go process vanishes from the process table. Used when
   stdout is not a TTY.

3. **`RunSubprocess` for TUI** — Go stays alive to tail `debug.log` and render
   the scrollable execution panel while Ansible runs.

4. **No `//nolint` comments** — use `.golangci.yml` exclusions with an
   explanation. See the root `README.md` for the full rule.

5. **Bubble Tea value receivers** — required by the Elm architecture.
   `hugeParam` warnings are excluded in `.golangci.yml` for all `internal/tui/`
   files. Do not convert to pointer receivers.

---

## Development

### Prerequisites

- Go 1.22+
- `golangci-lint` v1.64.8 (auto-installed by `make lint`)

### Build

```bash
cd cli
make build          # Build for current OS/arch → ../dist/valet
make build-all      # Cross-compile for all 4 platforms → ../dist/
make install        # Build + copy to /usr/local/valet-sh/bin/valet
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

All 17 commands follow the same pattern:

1. Parse CLI args with Cobra
2. Validate `.valet-sh.yml` if relevant (typed Go structs, clear errors)
3. Build extra-vars JSON for Ansible
4. On TTY: start `ansible.RunSubprocess()` + show execution panel
5. On non-TTY: call `ansible.Run()` which `syscall.Exec`s into `ansible-playbook`

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
| `internal/tui/exec.go` | `errcheck` | Best-effort file/viewport operations |
| `internal/tui/runner.go` | `errcheck` | Best-effort workdir lookup |
| `internal/tui/exec.go` | `gosec/G304` | `tailFile()` always receives the hardcoded `logPath` constant |
| All | `gosec/G204` | `syscall.Exec` with trusted argv is intentional |

---

## Security Notes

- `syscall.Exec` argv is constructed from platform constants + cobra-parsed args
- `tailFile()` path is always the hardcoded `logPath` constant, not user input
- `go.sum` pins exact SHA-256 hashes for all Go module dependencies
- GitHub Actions in CI use `@v4`/`@v5` tags — **todo: pin to commit SHAs**

---

## License

Apache 2.0 — see repository root for full license text.
Copyright 2025 TechDivision GmbH
