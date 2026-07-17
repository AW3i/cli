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
//
// Re-executing the original user command after a CLI update is the
// responsibility of the periodic check caller (check.go), not this function.
// Calling reExec here would cause an infinite loop when the user runs
// 'valet self-upgrade' directly.
func SelfUpgrade(currentVersion string, repoDir string) error {
	fmt.Println()
	fmt.Println(blue("▶ Checking for updates..."))
	fmt.Println()

	cliUpdated, cliErr := upgradeCliIfNeeded(currentVersion)
	if cliErr != nil {
		fmt.Fprintf(os.Stderr, "%s CLI update check failed: %v\n", ansiRed+"✘"+ansiReset, cliErr)
	}

	ansibleUpdated, ansibleErr := upgradeAnsibleIfNeeded(repoDir)
	if ansibleErr != nil {
		fmt.Fprintf(os.Stderr, "%s Ansible playbook update failed: %v\n", ansiRed+"✘"+ansiReset, ansibleErr)
	}

	// Runtime upgrade runs after the ansible pull so that if .runtime_version
	// changed in the playbook repo, we immediately install the new version.
	runtimeUpdated, runtimeErr := upgradeRuntimeIfNeeded(repoDir)
	if runtimeErr != nil {
		fmt.Fprintf(os.Stderr, "%s Runtime update failed: %v\n", ansiRed+"✘"+ansiReset, runtimeErr)
	}

	// Only say "everything is up to date" when all checks succeeded and
	// nothing was updated — not when any check failed.
	if cliErr == nil && ansibleErr == nil && runtimeErr == nil &&
		!cliUpdated && !ansibleUpdated && !runtimeUpdated {
		fmt.Println(green("✓ Everything is up to date."))
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

	latest, err := fetchLatestCliTag(upgradeAPITimeout)
	if err != nil {
		return false, err
	}

	if !isNewer(latest, currentVersion) {
		fmt.Printf("%s CLI is up to date (%s)\n", green("✓"), currentVersion)
		return false, nil
	}

	fmt.Printf("%s New CLI version available: %s → %s\n",
		blue("▶"), currentVersion, green(latest))

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	assetName := fmt.Sprintf("valet-%s-%s", goos, goarch)

	fmt.Printf("  Downloading %s...\n", assetName)
	binPath, tmpDir, err := downloadAndVerifyBinary(latest, assetName)
	if err != nil {
		return false, fmt.Errorf("download failed: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	installPath := "/usr/local/bin/valet.sh"
	fmt.Printf("  Installing to %s...\n", installPath)

	if err := os.MkdirAll(filepath.Dir(installPath), 0o755); err != nil {
		return false, fmt.Errorf("failed to create install directory: %w", err)
	}

	// Try direct install first; fall back to sudo when the path is root-owned.
	if err := installBinary(binPath, installPath); err != nil {
		return false, err
	}

	fmt.Printf("%s CLI updated to %s\n", green("✓"), latest)
	return true, nil
}

const (
	// runtimeRepo is the GitHub repo that publishes Python venv tarballs.
	runtimeRepo = "valet-sh/runtime"

	// runtimeInstallBase is the directory the runtime tarball is extracted into.
	// The tarball root is venv/, so the venv ends up at runtimeInstallBase/venv/.
	runtimeInstallBase = "/usr/local/valet-sh"

	// runtimeVersionFile records the currently installed runtime version.
	// Written after each successful runtime installation.
	runtimeVersionFile = "/usr/local/valet-sh/venv/.version"
)

// upgradeRuntimeIfNeeded compares the desired runtime version (from
// {repoDir}/.runtime_version) with the installed version, and downloads
// and extracts the new tarball when they differ.
//
// Note: valet-sh/runtime releases do not publish checksums — the download
// is not checksum-verified. This mirrors the behaviour of install.sh.
func upgradeRuntimeIfNeeded(repoDir string) (bool, error) {
	desiredFile := filepath.Join(repoDir, ".runtime_version")
	data, err := os.ReadFile(desiredFile)
	if err != nil {
		return false, fmt.Errorf("could not read .runtime_version: %w", err)
	}
	desired := strings.TrimSpace(string(data))
	if desired == "" {
		return false, fmt.Errorf(".runtime_version is empty")
	}

	installed := ""
	if d, err := os.ReadFile(runtimeVersionFile); err == nil {
		installed = strings.TrimSpace(string(d))
	}

	fmt.Printf("%s Checking for runtime updates...\n", blue("▶"))

	if installed == desired {
		fmt.Printf("%s Runtime is up to date (%s)\n", green("✓"), desired)
		return false, nil
	}

	if installed != "" {
		fmt.Printf("%s New runtime version available: %s → %s\n",
			blue("▶"), installed, green(desired))
	} else {
		fmt.Printf("%s Installing runtime %s...\n", blue("▶"), green(desired))
	}

	assetName, err := runtimeAssetName()
	if err != nil {
		return false, fmt.Errorf("could not determine runtime asset: %w", err)
	}

	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s",
		runtimeRepo, desired, assetName)

	fmt.Printf("  Downloading %s...\n", assetName)

	tmpDir, err := os.MkdirTemp("", "valet-runtime-*")
	if err != nil {
		return false, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarPath := filepath.Join(tmpDir, assetName)
	if err := downloadFile(downloadURL, tarPath); err != nil {
		return false, fmt.Errorf("failed to download runtime: %w", err)
	}

	fmt.Println("  Extracting runtime...")
	if err := extractTar(tarPath, runtimeInstallBase); err != nil {
		return false, err
	}

	if err := writeVersionFile(runtimeVersionFile, desired); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: could not save runtime version: %v\n", err)
	}

	fmt.Printf("%s Runtime updated to %s\n", green("✓"), desired)
	return true, nil
}

// runtimeAssetName returns the platform-specific tarball name for the current OS/arch.
// Linux:  ubuntu_{codename}-x86_64.tar.gz  (codename from /etc/os-release)
// macOS:  macos-{arm64|x86_64}.tar.gz
func runtimeAssetName() (string, error) {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}

	switch runtime.GOOS {
	case "darwin":
		return fmt.Sprintf("macos-%s.tar.gz", arch), nil
	default:
		codename, err := osCodename()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("ubuntu_%s-%s.tar.gz", codename, arch), nil
	}
}

// osCodename reads VERSION_CODENAME from /etc/os-release.
func osCodename() (string, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", fmt.Errorf("could not read /etc/os-release: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VERSION_CODENAME=") {
			return strings.Trim(strings.TrimPrefix(line, "VERSION_CODENAME="), `"`), nil
		}
	}
	return "", fmt.Errorf("VERSION_CODENAME not found in /etc/os-release")
}

// extractTar extracts a .tar.gz archive into destDir.
// Retries with sudo when the initial attempt fails (permission-protected paths).
func extractTar(tarPath, destDir string) error {
	cmd := exec.Command("tar", "-C", destDir, "-xzf", tarPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err == nil {
		return nil
	}
	fmt.Println("  Requesting sudo to extract runtime...")
	cmd = exec.Command("sudo", "tar", "-C", destDir, "-xzf", tarPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to extract runtime (sudo): %w", err)
	}
	return nil
}

// writeVersionFile writes version to path, creating parent directories as needed.
func writeVersionFile(path, version string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(version+"\n"), 0o644)
}

// upgradeAnsibleIfNeeded checks for updates to the Ansible playbook repo
// and pulls the latest changes if available. Returns true if an update was performed.
func upgradeAnsibleIfNeeded(repoDir string) (bool, error) {
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		fmt.Printf("%s Ansible playbooks are not in a git repo. Skipping update.\n", blue("ℹ"))
		return false, nil
	}

	fmt.Printf("%s Checking for Ansible playbook updates...\n", blue("▶"))
	// FIXME(revert-before-upstream-merge): tracks the fork's playbook branch
	// (playbookBranch, see check.go). Revert to "master" once merged upstream.
	cmd := exec.Command("git", "-C", repoDir, "fetch", "--quiet", "origin", playbookBranch)
	if err := cmd.Run(); err != nil {
		fmt.Printf("%s Could not fetch Ansible playbook updates\n", blue("ℹ"))
		return false, nil
	}

	localHeadCmd := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD")
	localHead, err := localHeadCmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to get local HEAD: %w", err)
	}

	remoteHeadCmd := exec.Command("git", "-C", repoDir, "rev-parse", "origin/"+playbookBranch)
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

	fmt.Println("  Pulling latest Ansible playbooks...")
	pullCmd := exec.Command("git", "-C", repoDir, "pull", "--quiet", "origin", playbookBranch)
	if err := pullCmd.Run(); err != nil {
		return false, fmt.Errorf("failed to pull Ansible playbooks: %w", err)
	}

	fmt.Printf("%s Ansible playbooks updated\n", green("✓"))
	return true, nil
}

// downloadAndVerifyBinary downloads the binary and checksums.txt from GitHub
// Releases, verifies the SHA256 checksum, and returns the path to the
// downloaded binary and the temp directory that contains it. The caller is
// responsible for cleaning up the temp directory after using the binary.
func downloadAndVerifyBinary(version, assetName string) (string, string, error) {
	// FIXME(revert-before-upstream-merge): uses the fork's release repo (cliRepo,
	// see check.go). Revert to "valet-sh/valet-sh-cli" once merged upstream.
	releaseURL := fmt.Sprintf("https://github.com/%s/releases/download/%s", cliRepo, version)

	tmpDir, err := os.MkdirTemp("", "valet-upgrade-*")
	if err != nil {
		return "", "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := downloadFile(releaseURL+"/checksums.txt", checksumsPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("failed to download checksums: %w", err)
	}

	binaryPath := filepath.Join(tmpDir, assetName)
	if err := downloadFile(releaseURL+"/"+assetName, binaryPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("failed to download binary: %w", err)
	}

	if err := verifySha256(binaryPath, checksumsPath, assetName); err != nil {
		os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("checksum verification failed: %w", err)
	}
	fmt.Printf("  %s Checksum verified\n", green("✓"))

	return binaryPath, tmpDir, nil
}

// installBinary copies src to installPath atomically.
// If the destination is not writable by the current user, it retries with sudo.
func installBinary(src, installPath string) error {
	tmpFile := installPath + ".tmp"

	err := copyFile(src, tmpFile)
	if err == nil {
		if err := os.Chmod(tmpFile, 0o755); err != nil {
			_ = os.Remove(tmpFile)
			return fmt.Errorf("failed to chmod binary: %w", err)
		}
		if err := os.Rename(tmpFile, installPath); err != nil {
			_ = os.Remove(tmpFile)
			return fmt.Errorf("failed to install binary: %w", err)
		}
		return nil
	}

	// Permission error — retry with sudo.
	if !os.IsPermission(err) {
		return fmt.Errorf("failed to stage binary: %w", err)
	}

	fmt.Println("  Requesting sudo to install to protected path...")
	cmd := exec.Command("sudo", "install", "-m", "755", src, installPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to install binary (sudo): %w", err)
	}
	return nil
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

// copyFile copies src to dst, creating dst if it does not exist.
// Used instead of os.Rename when src and dst may be on different filesystems.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}

// verifySha256 verifies the SHA256 checksum of a file against the checksums file.
// The checksums file should contain lines in the format: "sha256  filename"
func verifySha256(filePath, checksumsPath, expectedFileName string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	actualSha := hex.EncodeToString(h.Sum(nil))

	checksumsFile, err := os.Open(checksumsPath)
	if err != nil {
		return fmt.Errorf("failed to open checksums file: %w", err)
	}
	defer func() {
		_ = checksumsFile.Close()
	}()

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
