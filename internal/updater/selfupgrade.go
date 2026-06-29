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

package updater

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// SelfUpgrade checks for updates to both the CLI binary and the Ansible
// playbook repo, and applies them if newer versions are available.
func SelfUpgrade(currentVersion string, originalArgs []string, repoDir string) error {
	fmt.Println()
	fmt.Println(blue("▶ Checking for updates..."))
	fmt.Println()

	// Try to update CLI binary
	cliUpdated, err := upgradeCliIfNeeded(currentVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s CLI update check failed: %v\n", ansiRed+"✘"+ansiReset, err)
	}

	// Try to update Ansible playbook repo
	ansibleUpdated, err := upgradeAnsibleIfNeeded(repoDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s Ansible playbook update failed: %v\n", ansiRed+"✘"+ansiReset, err)
	}

	if !cliUpdated && !ansibleUpdated {
		fmt.Println(green("✓ Everything is up to date."))
		fmt.Println()
		return nil
	}

	// If CLI was updated, re-exec the original command with the new binary
	if cliUpdated {
		fmt.Println()
		fmt.Printf("%s Re-executing command with updated CLI...\n", blue("▶"))
		fmt.Println()
		reExec(originalArgs)
	}

	fmt.Println()
	return nil
}

// upgradeCliIfNeeded checks for a new CLI version and updates the binary
// if a newer version is available. Returns true if an update was performed.
func upgradeCliIfNeeded(currentVersion string) (bool, error) {
	if currentVersion == "dev" {
		fmt.Println(info("Development build detected. Skipping CLI update."))
		return false, nil
	}

	latest, err := fetchLatestCliTag()
	if err != nil {
		return false, err
	}

	if !isNewer(latest, currentVersion) {
		fmt.Printf("%s CLI is up to date (%s)\n", green("✓"), currentVersion)
		return false, nil
	}

	fmt.Printf("%s New CLI version available: %s → %s\n",
		blue("▶"), currentVersion, green(latest))

	// Detect OS and architecture
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	assetName := fmt.Sprintf("valet-%s-%s", goos, goarch)

	// Download binary and checksums
	fmt.Printf("  Downloading %s...\n", assetName)
	binPath, err := downloadAndVerifyBinary(latest, assetName)
	if err != nil {
		return false, fmt.Errorf("download failed: %w", err)
	}

	// Atomically replace the current binary
	installPath := "/usr/local/valet-sh/bin/valet"
	fmt.Printf("  Installing to %s...\n", installPath)

	// Create temp file in the same directory for atomic rename
	tmpFile := installPath + ".tmp"
	if err := os.Rename(binPath, tmpFile); err != nil {
		return false, fmt.Errorf("failed to move downloaded binary: %w", err)
	}

	// Make executable
	if err := os.Chmod(tmpFile, 0o755); err != nil {
		_ = os.Remove(tmpFile)
		return false, fmt.Errorf("failed to chmod binary: %w", err)
	}

	// Atomic rename (atomic on POSIX)
	if err := os.Rename(tmpFile, installPath); err != nil {
		_ = os.Remove(tmpFile)
		return false, fmt.Errorf("failed to install binary: %w", err)
	}

	fmt.Printf("%s CLI updated to %s\n", green("✓"), latest)
	return true, nil
}

// upgradeAnsibleIfNeeded checks for updates to the Ansible playbook repo
// and pulls the latest changes if available. Returns true if an update was performed.
func upgradeAnsibleIfNeeded(repoDir string) (bool, error) {
	// Check if the repo directory exists and is a git repo
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		// Not a git repo, skip update
		fmt.Printf("%s Ansible playbooks are not in a git repo. Skipping update.\n", blue("ℹ"))
		return false, nil
	}

	// Fetch from remote to check if there are updates
	fmt.Printf("%s Checking for Ansible playbook updates...\n", blue("▶"))
	cmd := exec.Command("git", "-C", repoDir, "fetch", "--quiet", "origin", "master")
	if err := cmd.Run(); err != nil {
		// Silently skip if fetch fails (network issue, no remote, etc.)
		fmt.Printf("%s Could not fetch Ansible playbook updates\n", blue("ℹ"))
		return false, nil
	}

	// Compare local HEAD with remote HEAD
	localHeadCmd := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD")
	localHead, err := localHeadCmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to get local HEAD: %w", err)
	}

	remoteHeadCmd := exec.Command("git", "-C", repoDir, "rev-parse", "origin/master")
	remoteHead, err := remoteHeadCmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to get remote HEAD: %w", err)
	}

	localHeadStr := strings.TrimSpace(string(localHead))
	remoteHeadStr := strings.TrimSpace(string(remoteHead))

	if localHeadStr == remoteHeadStr {
		fmt.Printf("%s Ansible playbooks are up to date\n", green("✓"))
		return false, nil
	}

	// Pull the latest changes
	fmt.Println("  Pulling latest Ansible playbooks...")
	pullCmd := exec.Command("git", "-C", repoDir, "pull", "--quiet", "origin", "master")
	if err := pullCmd.Run(); err != nil {
		return false, fmt.Errorf("failed to pull Ansible playbooks: %w", err)
	}

	fmt.Printf("%s Ansible playbooks updated\n", green("✓"))
	return true, nil
}

// downloadAndVerifyBinary downloads the binary and checksums.txt from GitHub Releases,
// verifies the SHA256 checksum, and returns the path to the downloaded binary.
func downloadAndVerifyBinary(version, assetName string) (string, error) {
	releaseURL := fmt.Sprintf("https://api.github.com/repos/valet-sh/valet-sh-cli/releases/download/%s", version)

	// Create a temporary directory for downloads
	tmpDir, err := os.MkdirTemp("", "valet-upgrade-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Download checksums.txt
	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := downloadFile(releaseURL+"/checksums.txt", checksumsPath); err != nil {
		return "", fmt.Errorf("failed to download checksums: %w", err)
	}

	// Download binary
	binaryPath := filepath.Join(tmpDir, assetName)
	if err := downloadFile(releaseURL+"/"+assetName, binaryPath); err != nil {
		return "", fmt.Errorf("failed to download binary: %w", err)
	}

	// Verify checksum
	if err := verifySha256(binaryPath, checksumsPath, assetName); err != nil {
		return "", fmt.Errorf("checksum verification failed: %w", err)
	}

	return binaryPath, nil
}

// downloadFile downloads a file from the given URL to the destination path.
func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = io.Copy(f, resp.Body)
	return err
}

// verifySha256 verifies the SHA256 checksum of a file against the checksums file.
// The checksums file should contain lines in the format: "sha256  filename"
func verifySha256(filePath, checksumsPath, expectedFileName string) error {
	// Read the file
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	// Compute SHA256
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	actualSha := hex.EncodeToString(h.Sum(nil))

	// Parse checksums file
	checksumsFile, err := os.Open(checksumsPath)
	if err != nil {
		return fmt.Errorf("failed to open checksums file: %w", err)
	}
	defer func() {
		_ = checksumsFile.Close()
	}()

	// Find the line matching the expected filename
	scanner := bufio.NewScanner(checksumsFile)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[1] == expectedFileName {
			expectedSha := parts[0]
			if actualSha != expectedSha {
				return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSha, actualSha)
			}
			return nil
		}
	}

	return fmt.Errorf("checksum for %s not found in checksums.txt", expectedFileName)
}
