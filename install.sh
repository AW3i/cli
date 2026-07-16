#!/usr/bin/env bash
##
#   Copyright 2025 TechDivision GmbH
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.
##
#
# valet.sh installer — installs all three components of valet.sh:
#   1. Runtime venv  (valet-sh/runtime)     -> /usr/local/valet-sh/venv
#   2. Ansible repo  (playbooks)            -> /usr/local/valet-sh/valet-sh
#   3. Go CLI binary (release asset)        -> /usr/local/valet-sh/bin/valet
# and a `valet.sh` shim on PATH.
#
# FIXME(revert-before-upstream-merge): defaults below point at the AW3i fork
# (CLI repo AW3i/cli, playbooks AW3i/valet-sh @ 3.x) for testing. Once merged
# upstream, change the defaults back to valet-sh/valet-sh-cli + valet-sh/valet-sh
# @ master. All values are overridable via environment variables.

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[1;34m'
NC='\033[0m'

# ---------------------------------------------------------------------------
# Configuration (env-overridable)
# ---------------------------------------------------------------------------
VSH_CLI_REPO="${VSH_CLI_REPO:-AW3i/cli}"
VSH_PLAYBOOK_REPO="${VSH_PLAYBOOK_REPO:-AW3i/valet-sh}"
VSH_PLAYBOOK_BRANCH="${VSH_PLAYBOOK_BRANCH:-3.x}"
VSH_RUNTIME_REPO="${VSH_RUNTIME_REPO:-valet-sh/runtime}"

INSTALL_BASE="/usr/local/valet-sh"
INSTALL_BIN="${INSTALL_BASE}/bin/valet"
VENV_DIR="${INSTALL_BASE}/venv"
PLAYBOOK_DIR="${INSTALL_BASE}/valet-sh"
SYMLINK="/usr/local/bin/valet.sh"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()  { echo -e "${BLUE}▶${NC} $*"; }
ok()    { echo -e "${GREEN}✓${NC} $*"; }
err()   { echo -e "${RED}✘${NC} $*" >&2; }
die()   { err "$*"; exit 1; }

# Run a command as root when not already root (uses sudo if available).
as_root() {
    if [ "$(id -u)" -eq 0 ]; then
        "$@"
    elif command -v sudo >/dev/null 2>&1; then
        sudo "$@"
    else
        die "root privileges required for: $*"
    fi
}

# ---------------------------------------------------------------------------
# Detect OS / architecture
# ---------------------------------------------------------------------------
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64) GOARCH="amd64"; RT_ARCH="x86_64" ;;
    aarch64|arm64) GOARCH="arm64"; RT_ARCH="arm64" ;;
    *) die "Unsupported architecture: $ARCH" ;;
esac

case "$OS" in
    linux) OS="linux" ;;
    darwin) OS="darwin" ;;
    *) die "Unsupported OS: $OS" ;;
esac

PLATFORM="${OS}-${GOARCH}"
BINARY_NAME="valet-${PLATFORM}"

echo -e "${BLUE}▶ valet.sh installer${NC}"
echo "  CLI repo:      ${VSH_CLI_REPO}"
echo "  Playbooks:     ${VSH_PLAYBOOK_REPO}@${VSH_PLAYBOOK_BRANCH}"
echo "  Runtime repo:  ${VSH_RUNTIME_REPO}"
echo

# ---------------------------------------------------------------------------
# 1. Dependencies (Linux/apt only; macOS assumes brew/git present)
# ---------------------------------------------------------------------------
if [ "$OS" = "linux" ] && command -v apt-get >/dev/null 2>&1; then
    info "Installing base dependencies (git, python3, python3-venv, curl, tar)"
    as_root apt-get update -y >/dev/null
    as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        git python3 python3-venv curl ca-certificates tar >/dev/null
fi

# ---------------------------------------------------------------------------
# Prepare install directory
# ---------------------------------------------------------------------------
info "Preparing ${INSTALL_BASE}"
as_root mkdir -p "${INSTALL_BASE}/bin" "${INSTALL_BASE}/etc"
# Make the tree writable for the current user so the rest runs without sudo.
as_root chown -R "$(id -un)":"$(id -gn)" "${INSTALL_BASE}"

# ---------------------------------------------------------------------------
# 2. Playbooks (clone first — we need .runtime_version from it)
# ---------------------------------------------------------------------------
if [ -d "${PLAYBOOK_DIR}/.git" ]; then
    info "Updating playbooks (${VSH_PLAYBOOK_REPO}@${VSH_PLAYBOOK_BRANCH})"
    git -C "${PLAYBOOK_DIR}" fetch --quiet origin "${VSH_PLAYBOOK_BRANCH}"
    git -C "${PLAYBOOK_DIR}" checkout --quiet "${VSH_PLAYBOOK_BRANCH}"
    git -C "${PLAYBOOK_DIR}" reset --hard --quiet "origin/${VSH_PLAYBOOK_BRANCH}"
else
    info "Cloning playbooks (${VSH_PLAYBOOK_REPO}@${VSH_PLAYBOOK_BRANCH})"
    git clone --quiet --branch "${VSH_PLAYBOOK_BRANCH}" \
        "https://github.com/${VSH_PLAYBOOK_REPO}.git" "${PLAYBOOK_DIR}" \
        || die "failed to clone playbooks"
fi
ok "Playbooks ready at ${PLAYBOOK_DIR}"

# ---------------------------------------------------------------------------
# 3. Runtime venv (Python + Ansible + pip deps, e.g. beautifultable)
# ---------------------------------------------------------------------------
RUNTIME_VERSION="$(tr -d ' \t\n\r' < "${PLAYBOOK_DIR}/.runtime_version")"
[ -n "${RUNTIME_VERSION}" ] || die "could not read .runtime_version from playbooks"

if [ "$OS" = "linux" ]; then
    # Ubuntu package name is ubuntu_<codename>-<arch>
    CODENAME="$(. /etc/os-release && echo "${VERSION_CODENAME:-}")"
    [ -n "${CODENAME}" ] || die "could not determine Ubuntu codename from /etc/os-release"
    RUNTIME_PKG="ubuntu_${CODENAME}-${RT_ARCH}"
else
    RUNTIME_PKG="macos-${RT_ARCH}"
fi

RUNTIME_URL="https://github.com/${VSH_RUNTIME_REPO}/releases/download/${RUNTIME_VERSION}/${RUNTIME_PKG}.tar.gz"
info "Installing runtime ${RUNTIME_VERSION} (${RUNTIME_PKG})"

TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TEMP_DIR}"' EXIT

if ! curl -fL --progress-bar -o "${TEMP_DIR}/runtime.tar.gz" "${RUNTIME_URL}"; then
    die "failed to download runtime from ${RUNTIME_URL}"
fi
# The tarball's top-level directory is 'venv/', so extract into INSTALL_BASE.
rm -rf "${VENV_DIR}"
tar -C "${INSTALL_BASE}" -xzf "${TEMP_DIR}/runtime.tar.gz" || die "failed to extract runtime"
[ -x "${VENV_DIR}/bin/ansible-playbook" ] \
    || die "runtime venv missing ansible-playbook (unexpected tarball layout)"
ok "Runtime venv installed at ${VENV_DIR}"

# ---------------------------------------------------------------------------
# 4. CLI binary (from GitHub release of the CLI repo)
# ---------------------------------------------------------------------------
RELEASE_URL="https://api.github.com/repos/${VSH_CLI_REPO}/releases/latest"
info "Fetching latest ${VSH_CLI_REPO} release"
RELEASE_JSON="$(curl -fsSL -H "Accept: application/vnd.github+json" "${RELEASE_URL}")" \
    || die "failed to query releases API for ${VSH_CLI_REPO}"
TAG="$(echo "${RELEASE_JSON}" | grep -o '"tag_name":[[:space:]]*"[^"]*"' | head -1 | cut -d'"' -f4)"
[ -n "${TAG}" ] || die "no release found for ${VSH_CLI_REPO} (have you pushed a v* tag?)"
ok "Latest release: ${TAG}"

DOWNLOAD_BASE="https://github.com/${VSH_CLI_REPO}/releases/download/${TAG}"
info "Downloading ${BINARY_NAME}"
curl -fsSL -o "${TEMP_DIR}/${BINARY_NAME}" "${DOWNLOAD_BASE}/${BINARY_NAME}" \
    || die "failed to download ${BINARY_NAME}"
curl -fsSL -o "${TEMP_DIR}/checksums.txt" "${DOWNLOAD_BASE}/checksums.txt" \
    || die "failed to download checksums.txt"

# Verify checksum
EXPECTED_SHA="$(grep "${BINARY_NAME}" "${TEMP_DIR}/checksums.txt" | awk '{print $1}')"
[ -n "${EXPECTED_SHA}" ] || die "checksum not found for ${BINARY_NAME}"
if command -v sha256sum >/dev/null 2>&1; then
    ACTUAL_SHA="$(sha256sum "${TEMP_DIR}/${BINARY_NAME}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
    ACTUAL_SHA="$(shasum -a 256 "${TEMP_DIR}/${BINARY_NAME}" | awk '{print $1}')"
else
    die "sha256sum or shasum not found"
fi
[ "${EXPECTED_SHA}" = "${ACTUAL_SHA}" ] || die "checksum verification failed for ${BINARY_NAME}"
ok "Checksum verified"

install -m 0755 "${TEMP_DIR}/${BINARY_NAME}" "${INSTALL_BIN}"
ok "CLI installed to ${INSTALL_BIN}"

# ---------------------------------------------------------------------------
# 5. Shim on PATH
# ---------------------------------------------------------------------------
as_root rm -f "${SYMLINK}"
as_root tee "${SYMLINK}" >/dev/null <<EOF
#!/bin/bash
exec ${INSTALL_BIN} "\$@"
EOF
as_root chmod 0755 "${SYMLINK}"
ok "Shim created at ${SYMLINK}"

# ---------------------------------------------------------------------------
# Verify
# ---------------------------------------------------------------------------
echo
"${INSTALL_BIN}" --version >/dev/null 2>&1 || die "installation test failed"
ok "$("${INSTALL_BIN}" --version)"
echo
ok "valet.sh installation complete!"
echo "  Run: valet.sh --help"
