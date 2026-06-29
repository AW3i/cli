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

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[1;34m'
NC='\033[0m' # No Color

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

# Map architecture names
case "$ARCH" in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64)
        ARCH="arm64"
        ;;
esac

# Map OS names
case "$OS" in
    linux)
        OS="linux"
        ;;
    darwin)
        OS="darwin"
        ;;
    *)
        echo -e "${RED}✘ Unsupported OS: $OS${NC}"
        exit 1
        ;;
esac

PLATFORM="${OS}-${ARCH}"
BINARY_NAME="valet-${PLATFORM}"
INSTALL_BASE="/usr/local/valet-sh"
INSTALL_BIN="${INSTALL_BASE}/bin/valet"

# GitHub release constants
GITHUB_REPO="valet-sh/valet-sh-cli"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}"
RELEASE_URL="${GITHUB_API}/releases/latest"

echo -e "${BLUE}▶ valet.sh installer${NC}"
echo

# Fetch latest release info
echo "Fetching latest valet-sh CLI release..."
RELEASE_JSON=$(curl -s -H "Accept: application/vnd.github+json" "${RELEASE_URL}")
VERSION=$(echo "${RELEASE_JSON}" | grep -o '"tag_name":"[^"]*"' | head -1 | cut -d'"' -f4 | sed 's/^v//')

if [ -z "$VERSION" ]; then
    echo -e "${RED}✘ Failed to determine latest version from GitHub${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Latest version: ${VERSION}${NC}"
echo

# Download binary and checksums
DOWNLOAD_BASE="https://github.com/${GITHUB_REPO}/releases/download/v${VERSION}"

# Create temporary directory
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

echo "Downloading binary for ${PLATFORM}..."
if ! curl -sL -o "${TEMP_DIR}/${BINARY_NAME}" "${DOWNLOAD_BASE}/${BINARY_NAME}"; then
    echo -e "${RED}✘ Failed to download binary${NC}"
    exit 1
fi

echo "Downloading checksums..."
if ! curl -sL -o "${TEMP_DIR}/checksums.txt" "${DOWNLOAD_BASE}/checksums.txt"; then
    echo -e "${RED}✘ Failed to download checksums${NC}"
    exit 1
fi

# Verify checksum
echo "Verifying checksum..."
cd "${TEMP_DIR}"

# Extract the expected checksum for our binary
EXPECTED_SHA=$(grep "${BINARY_NAME}" checksums.txt | awk '{print $1}')

if [ -z "$EXPECTED_SHA" ]; then
    echo -e "${RED}✘ Checksum not found for ${BINARY_NAME}${NC}"
    exit 1
fi

# Calculate actual checksum
if command -v sha256sum &> /dev/null; then
    ACTUAL_SHA=$(sha256sum "${BINARY_NAME}" | awk '{print $1}')
elif command -v shasum &> /dev/null; then
    ACTUAL_SHA=$(shasum -a 256 "${BINARY_NAME}" | awk '{print $1}')
else
    echo -e "${RED}✘ sha256sum or shasum not found${NC}"
    exit 1
fi

if [ "$EXPECTED_SHA" != "$ACTUAL_SHA" ]; then
    echo -e "${RED}✘ Checksum verification failed${NC}"
    echo "Expected: $EXPECTED_SHA"
    echo "Got:      $ACTUAL_SHA"
    exit 1
fi

echo -e "${GREEN}✓ Checksum verified${NC}"
echo

# Create installation directory
echo "Installing valet-sh CLI..."
if [ ! -d "${INSTALL_BASE}" ]; then
    echo "Creating ${INSTALL_BASE}..."
    mkdir -p "${INSTALL_BASE}"/{bin,etc,valet-sh}
fi

# Create subdirectories if they don't exist
mkdir -p "${INSTALL_BASE}"/{bin,etc,valet-sh}

# Install binary
install -m 755 "${TEMP_DIR}/${BINARY_NAME}" "${INSTALL_BIN}"
echo -e "${GREEN}✓ CLI installed to ${INSTALL_BIN}${NC}"

# Clone or pull valet-sh Ansible repository
if [ -d "${INSTALL_BASE}/valet-sh/.git" ]; then
    echo "Updating valet-sh Ansible repository..."
    git -C "${INSTALL_BASE}/valet-sh" pull --quiet origin master || true
else
    echo "Cloning valet-sh Ansible repository..."
    if ! git clone --quiet --branch master https://github.com/valet-sh/valet-sh.git "${INSTALL_BASE}/valet-sh"; then
        echo -e "${BLUE}ℹ Warning: Failed to clone valet-sh Ansible repository${NC}"
        echo "  You can manually clone it later:"
        echo "  git clone https://github.com/valet-sh/valet-sh.git ${INSTALL_BASE}/valet-sh"
    fi
fi

# Create symlink to /usr/local/bin if it doesn't exist or is wrong
SYMLINK="/usr/local/bin/valet.sh"
if [ -L "$SYMLINK" ] || [ -f "$SYMLINK" ]; then
    rm -f "$SYMLINK"
fi

# Create a shim script instead of a symlink
cat > "$SYMLINK" << 'EOF'
#!/bin/bash
exec /usr/local/valet-sh/bin/valet "$@"
EOF
chmod 755 "$SYMLINK"

echo -e "${GREEN}✓ Symlink created at ${SYMLINK}${NC}"
echo

# Test the installation
echo "Testing installation..."
if ! "${INSTALL_BIN}" --version > /dev/null 2>&1; then
    echo -e "${RED}✘ Installation test failed${NC}"
    exit 1
fi

VERSION_OUTPUT=$("${INSTALL_BIN}" --version)
echo -e "${GREEN}✓ ${VERSION_OUTPUT}${NC}"
echo

echo -e "${GREEN}✓ Installation complete!${NC}"
echo
echo "Next steps:"
echo "  1. Ensure /usr/local/bin is in your PATH"
echo "  2. Run: valet.sh --help"
echo "  3. Create a .valet-sh.yml in your project directory"
echo
echo "For more information, visit: https://valet-sh.io"
