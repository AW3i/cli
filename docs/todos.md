# Open TODOs

## Bugs

### B1 — `waitForFirstJSONTask` break-in-switch (regression)
**Priority: High — I introduced this**

When the if-else chain was converted to a switch in commit `1e4e61a`, the `break` inside
`case '\n'` now exits the switch instead of the for loop. The function is supposed to return
as soon as the first task-start JSON event is seen, but instead reads until the pipe closes
(EOF). Result: the TUI exec panel only appears after the entire playbook finishes — no live
progress.

Fix: use a labeled break (`break loop`) or revert this case to `if/else`.
File: `internal/tui/runner.go:88-99`

### B2 — non-TTY path drops playbook options
**Priority: Medium — pre-existing, behavior-changing to fix**

The cobra `RunE` built in `buildCommand` only passes `Args` to `ansible.Run`, never `Opts`.
So `-d`, `--skip-fs`, `--skip-db` etc. are silently dropped (or rejected by cobra) when
running off-TTY (CI, pipes). The TUI panel path (`resolveRunOpts`) handles these correctly.

Fix: add `DisableFlagParsing` to discovered commands + reuse the same token-splitting logic
from `resolveRunOpts`.
File: `internal/commands/discover.go:144-165`

### B3 — `writeTimestamp` silent failure → API hammering
**Priority: Medium — pre-existing, behavior-changing to fix**

If `/usr/local/valet-sh/etc/.last_update_check` is not writable, `os.OpenFile` errors and
the function silently returns. The timestamp is never written → `checkDue()` always returns
`true` → the GitHub API is hit on every `valet` invocation, defeating the 7-day throttle.

Fix: log a warning on failure, or fall back to a user-writable location.
File: `internal/updater/check.go:210-217`

### B4 — ARM-Linux runtime asset name 404
**Priority: Low — edge case, pre-existing**

`runtimeAssetName()` only remaps `amd64 → x86_64`. On ARM64 Linux it produces
`ubuntu_noble-arm64.tar.gz`, which `valet-sh/runtime` does not publish (only x86_64 for
Linux). Results in a silent 404 with no "unsupported platform" message.

Fix: validate supported OS/arch combinations and return a clear error for unsupported ones.
File: `internal/updater/selfupgrade.go:206-222`

### B5 — `parseSemver` swallows malformed tags as `0.0.0`
**Priority: Low-Medium — pre-existing**

`strconv.Atoi` errors are discarded. A malformed upstream tag (e.g. empty string, `latest`)
parses as `[0,0,0]`, which is never newer than any real version → a legitimate update is
silently suppressed with no signal.

Fix: return an error or valid flag from `parseSemver`; have `isNewer` treat unparseable input
conservatively and log it.
File: `internal/updater/check.go:311-325`

---

## Dead Code (safe to remove, no behavior change)

- `internal/config/global.go` — `LoadGlobal`, `GlobalConfig`, `GlobalConfigDir`, all struct
  fields; no external references anywhere (~35 lines)
- `internal/commands/helpers.go` — empty file (license header only); its test file tests
  `ErrorPrefix` which lives in `help.go`
- `internal/tui/styles.go` — `Divider` field: initialized in `newStyles()`, never rendered
- `internal/tui/list.go:73-112` — `CommandItem.Args()`, `ArgDef`, `argsFromUse`: only
  referenced by one test (`launcher_test.go:361-382`); the inline box uses free-form input,
  structured arg parsing was abandoned

---

## Refactors (behavior-preserving)

- **Git head-compare duplication**: `check.go:159-186` and `selfupgrade.go:268-312` are
  identical fetch + rev-parse sequences. Extract
  `fetchBranch(repoDir, branch)` + `localRemoteHeads(repoDir, branch)` helpers. Also reduces
  the FIXME-revert surface from 2 places to 1.
- **ANSI color constants**: byte-identical in `updater/check.go:74` and
  `commands/help.go:29`; `blue`/`green` in both (with `green` subtly different — updater adds
  bold). Create `internal/ansicolor` package and have both consumers use it.
- **Ansible runner helpers**: `ExtraVars` build + nil-normalize + marshal, playbook-path
  resolution, and workDir resolution all duplicated between `Run` and `RunSubprocess` in
  `internal/ansible/runner.go`. Extract into shared private helpers.
- **Named `downloadTimeout`**: `downloadFile` uses magic `30 * time.Second`; add a named
  const alongside `apiTimeout` and `upgradeAPITimeout` in `check.go`.
- **`executeViaCobra` helper**: `os.Args = append(…)` + `root.Execute()` repeated 4× in
  `runner.go:42-60`. Extract into one function; prefer `root.SetArgs(args)` over mutating
  global `os.Args`.
- **`setEnv` uses hand-rolled `HasPrefix`**: `ansible/runner.go:239` — replace with
  `strings.HasPrefix`.
- **Ansible event-name string literals**: `"v2_playbook_on_task_start"` etc. appear in both
  `ansible_events.go` and `runner.go`. Define once as named constants.

---

## Bootstrap / install.sh
**Needs team discussion**

The `install.sh` in this repo was written from scratch as a custom bootstrap. The original
upstream installer (`valet-sh/install`) delegates to a compiled `valet-sh-installer` binary
that installs the old Python CLI — it knows nothing about the new Go binary.

The Go binary is fully static (no system lib deps) and handles HTTP natively. The only real
system dependencies for a fresh install are `git` and `tar`, both ubiquitous on developer
machines.

**Options:**
- **A** — Keep `install.sh` as a convenience wrapper (binary download + `self-upgrade`)
- **B** — Remove `install.sh`; document a two-command bootstrap:
  ```bash
  sudo curl -fsSL https://github.com/.../releases/latest/download/valet-linux-amd64 \
    -o /usr/local/bin/valet.sh && sudo chmod 755 /usr/local/bin/valet.sh
  VALET_UPDATE_CHANNEL=dev valet.sh self-upgrade
  ```
- **C** — Update `valet-sh/install` to download the Go binary instead of invoking
  `valet-sh-installer setup`

**What self-upgrade needs for B/C:**
- Detect missing playbook dir → `git clone` instead of `git pull` (currently assumes the
  repo exists)

---

## Upstream Merge (FIXME markers)

Three places point at the AW3i fork for testing. Revert before upstream merge:

| File | Change needed |
|---|---|
| `internal/updater/check.go` | `cliRepo` → `"valet-sh/valet-sh-cli"`, `playbookBranch` → `"master"` |
| `internal/updater/selfupgrade.go` | same constants |
| `install.sh` | `VSH_CLI_REPO`, `VSH_PLAYBOOK_REPO`, `VSH_PLAYBOOK_BRANCH` |

---

## CI / Actions

- `actions/setup-go@v6.5.0` still emits a Node.js 20 deprecation warning (non-critical, not
  a failure). Fix by bumping to a v6 patch that targets Node.js 24 once one is published, or
  skip to v7.

---

## Product / Integration

- **Python CLI → Go CLI cutover**: confirmed direction. Installer, docs, and upstream repos
  still reference the Python CLI.
- **Self-upgrade end-to-end test on a clean release build**: recent testing ran an old local
  `make build` (commit `244056e`, `3.0.2-dev` era) via a dev shim — not current code.
