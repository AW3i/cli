# Open TODOs

## Bootstrap / install.sh

**Context:**  
The `install.sh` in this repo was written from scratch by us as a custom bootstrap. The original upstream installer lives at `valet-sh/install` and delegates to a compiled `valet-sh-installer` binary — but that flow installs the old Python CLI, not the new Go binary.

Since the Go binary is fully static (no system lib deps) and handles HTTP natively, the only real system dependencies for a fresh install are `git` (to clone the playbook repo) and `tar` (to extract the runtime venv) — both ubiquitous on developer machines.

**Decision needed (discuss with other dev):**

Should we:

- **A — Keep `install.sh`** as a convenience script (wraps a binary download + `self-upgrade`)
- **B — Remove `install.sh`** and document a two-command bootstrap:
  ```bash
  sudo curl -fsSL https://github.com/.../releases/latest/download/valet-linux-amd64 \
    -o /usr/local/bin/valet.sh && sudo chmod 755 /usr/local/bin/valet.sh
  VALET_UPDATE_CHANNEL=dev valet.sh self-upgrade
  ```
- **C — Update the original `valet-sh/install` installer** to download the Go binary instead of invoking `valet-sh-installer setup`

**What self-upgrade still needs for option B/C:**
- Detect missing playbook dir → `git clone` instead of `git pull` (currently assumes repo exists)

## Upstream merge (FIXME markers)

Three places in the code are tagged `FIXME(revert-before-upstream-merge)` pointing at the AW3i fork instead of upstream repos:

- `internal/updater/check.go` — `cliRepo = "AW3i/cli"`, `playbookBranch = "3.x"`
- `internal/updater/selfupgrade.go` — same constants
- `install.sh` — `VSH_CLI_REPO`, `VSH_PLAYBOOK_REPO`, `VSH_PLAYBOOK_BRANCH`

Once changes are merged upstream, revert to `valet-sh/valet-sh-cli` + `valet-sh/valet-sh` @ `master`.

## GitHub Actions — Node.js 20 (non-critical)

`actions/setup-go@v6.5.0` still shows a Node.js 20 deprecation warning. Not a CI failure. Fix when a Node.js 24-native v6 patch is available, or bump to v7.
