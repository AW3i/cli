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

// Package updater performs a periodic background check for new valet-sh-cli
// releases and valet-sh playbook updates, prompting the user when available.
package updater

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// FIXME(revert-before-upstream-merge): the updater is temporarily pointed at the
// AW3i fork (CLI repo AW3i/cli, playbooks branch 3.x) so self-upgrade can be
// tested against the fork. Once these changes are reviewed and merged into the
// upstream project, revert cliRepo to "valet-sh/valet-sh-cli" and
// playbookBranch to "master". See also selfupgrade.go which uses these consts.
const (
	// cliRepo is the GitHub repo (owner/name) that publishes the CLI releases.
	cliRepo = "AW3i/cli"
	// playbookBranch is the branch of the valet-sh Ansible repo to track.
	playbookBranch = "3.x"
)

const (
	// checkInterval is how often the GitHub API is consulted.
	checkInterval = 7 * 24 * time.Hour

	// timestampFile records the last time the check ran.
	timestampFile = "/usr/local/valet-sh/etc/.last_update_check"

	// cliReleaseStableURL returns the latest non-prerelease, non-draft release.
	cliReleaseStableURL = "https://api.github.com/repos/" + cliRepo + "/releases/latest"

	// cliReleasesURL returns all releases (including pre-releases) ordered by
	// created_at descending. Used by the dev channel.
	cliReleasesURL = "https://api.github.com/repos/" + cliRepo + "/releases"

	// apiTimeout caps the background-check HTTP call so a slow network never
	// blocks the user mid-command.
	apiTimeout = 3 * time.Second

	// upgradeAPITimeout is used for explicit 'valet self-upgrade' invocations
	// where the user is actively waiting and a longer round-trip is acceptable.
	upgradeAPITimeout = 15 * time.Second

	// UpdateChannelEnvVar is the environment variable that controls which
	// release channel is tracked. Accepted values: "stable" (default), "dev".
	UpdateChannelEnvVar = "VALET_UPDATE_CHANNEL"
)

// ANSI codes — same values as the Python callback plugin and help.go.
const (
	ansiRed   = "\033[1;31m"
	ansiBlue  = "\033[1;34m"
	ansiGreen = "\033[0;32m"
	ansiBold  = "\033[;1m"
	ansiReset = "\033[0;0m"
)

func blue(s string) string  { return ansiBlue + ansiBold + s + ansiReset }
func green(s string) string { return ansiGreen + ansiBold + s + ansiReset }
func info(s string) string  { return ansiBlue + "ℹ " + s + ansiReset }

// releaseResponse is the subset of the GitHub releases API we care about.
type releaseResponse struct {
	TagName string `json:"tag_name"`
	Draft   bool   `json:"draft"`
}

// Check runs the periodic update check for both CLI and Ansible playbook repo.
// It is a no-op when:
//   - the check was run less than checkInterval ago
//   - the GitHub API is unreachable (fails silently)
//   - currentVersion is "dev" (local development build)
//
// originalArgs is os.Args so the command can be re-executed after a CLI update.
// repoDir is the path to the valet-sh Ansible repo (typically platform.RepoDir()).
func Check(currentVersion string, originalArgs []string, repoDir string) {
	if currentVersion == "dev" {
		return
	}

	if !checkDue() {
		return
	}

	// Always write the timestamp first so a network error doesn't cause
	// the check to hammer the API on every subsequent invocation.
	writeTimestamp()

	cliNewer, cliLatest := checkCliUpdate(currentVersion)
	ansibleNewer := checkAnsibleUpdate(repoDir)

	if !cliNewer && !ansibleNewer {
		return
	}

	fmt.Println()

	cliUpdated := false
	if cliNewer {
		printCliUpdatePrompt(currentVersion, cliLatest)
		if askYesNo() {
			fmt.Println()
			cliUpdated = promptSelfUpgrade()
		} else {
			fmt.Println(info("Skipping. Run 'valet self-upgrade' to upgrade anytime."))
			fmt.Println()
		}
	}

	if ansibleNewer && !cliUpdated {
		printAnsibleUpdatePrompt()
		if askYesNo() {
			fmt.Println()
			promptSelfUpgrade()
		} else {
			fmt.Println(info("Skipping. Run 'valet self-upgrade' to upgrade anytime."))
			fmt.Println()
		}
	}

	if cliUpdated {
		reExec(originalArgs)
	}
}

// checkCliUpdate returns true if a newer CLI version is available, along with the latest tag.
func checkCliUpdate(currentVersion string) (bool, string) {
	latest, err := fetchLatestCliTag(apiTimeout)
	if err != nil {
		return false, ""
	}
	return isNewer(latest, currentVersion), latest
}

// checkAnsibleUpdate returns true if the Ansible repo has updates available.
func checkAnsibleUpdate(repoDir string) bool {
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return false
	}

	cmd := exec.Command("git", "-C", repoDir, "fetch", "--quiet", "origin", playbookBranch)
	if err := cmd.Run(); err != nil {
		return false
	}

	localHeadCmd := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD")
	localHead, err := localHeadCmd.Output()
	if err != nil {
		return false
	}

	remoteHeadCmd := exec.Command("git", "-C", repoDir, "rev-parse", "origin/"+playbookBranch)
	remoteHead, err := remoteHeadCmd.Output()
	if err != nil {
		return false
	}

	localHeadStr := strings.TrimSpace(string(localHead))
	remoteHeadStr := strings.TrimSpace(string(remoteHead))

	return localHeadStr != remoteHeadStr
}

// promptSelfUpgrade calls valet self-upgrade and returns true if the CLI was updated.
func promptSelfUpgrade() bool {
	cmd := exec.Command("valet", "self-upgrade")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s self-upgrade failed: %v\n", ansiRed+"✘"+ansiReset, err)
		return false
	}
	return true
}

// checkDue returns true if the timestamp file is missing or older than checkInterval.
func checkDue() bool {
	fi, err := os.Stat(timestampFile)
	if err != nil {
		return true
	}
	return time.Since(fi.ModTime()) >= checkInterval
}

// writeTimestamp touches the timestamp file, creating it if necessary.
func writeTimestamp() {
	f, err := os.OpenFile(timestampFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return
	}
	_ = f.Close()
}

// updateChannel returns the release channel to track.
// Defaults to "stable"; set VALET_UPDATE_CHANNEL=dev to include pre-releases.
func updateChannel() string {
	if strings.ToLower(os.Getenv(UpdateChannelEnvVar)) == "dev" {
		return "dev"
	}
	return "stable"
}

// fetchLatestCliTag returns the latest tag for the configured channel using
// the given HTTP timeout. Pass apiTimeout for background checks, upgradeAPITimeout
// for explicit self-upgrade invocations.
func fetchLatestCliTag(timeout time.Duration) (string, error) {
	if updateChannel() == "dev" {
		return fetchLatestAnyTag(timeout)
	}
	return fetchLatestStableTag(timeout)
}

// fetchLatestStableTag queries /releases/latest — GitHub only returns
// non-prerelease, non-draft releases from this endpoint.
func fetchLatestStableTag(timeout time.Duration) (string, error) {
	resp, err := githubGet(cliReleaseStableURL, timeout)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var rel releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return strings.TrimPrefix(rel.TagName, "v"), nil
}

// fetchLatestAnyTag queries /releases (full list, newest first) and returns
// the first non-draft entry — stable or pre-release.
func fetchLatestAnyTag(timeout time.Duration) (string, error) {
	resp, err := githubGet(cliReleasesURL, timeout)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var releases []releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", err
	}

	for _, r := range releases {
		if !r.Draft {
			return strings.TrimPrefix(r.TagName, "v"), nil
		}
	}
	return "", fmt.Errorf("no published releases found")
}

// githubGet performs a GET request to the GitHub API with the standard headers.
func githubGet(url string, timeout time.Duration) (*http.Response, error) {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	return client.Do(req)
}

// isNewer returns true when candidate is a higher semver than current.
// Both strings should be in the form "MAJOR.MINOR.PATCH" (no "v" prefix).
func isNewer(candidate, current string) bool {
	c := parseSemver(candidate)
	v := parseSemver(current)
	for i := range c {
		if c[i] > v[i] {
			return true
		}
		if c[i] < v[i] {
			return false
		}
	}
	return false
}

// parseSemver splits a version string into [major, minor, patch] ints.
// Non-numeric pre-release suffixes (e.g. "-99-gabcdef") are stripped.
func parseSemver(v string) [3]int {
	// Strip any git-describe suffix (e.g. "2.9.19-101-gabcdef").
	v = strings.SplitN(v, "-", 2)[0]
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		result[i], _ = strconv.Atoi(p)
	}
	return result
}

// IsHelpOrVersionCall returns true when the user is asking for help or
// version info — cases where an interactive update prompt is unwelcome.
func IsHelpOrVersionCall(args []string) bool {
	for _, a := range args[1:] {
		switch a {
		case "--help", "-h", "--version", "-v", "help":
			return true
		}
	}
	return false
}

// IsSelfUpgradeCall returns true when the user is running 'valet self-upgrade'
// directly — the periodic check should not run in this case since self-upgrade
// is already the update mechanism.
func IsSelfUpgradeCall(args []string) bool {
	for _, a := range args[1:] {
		if a == "self-upgrade" {
			return true
		}
	}
	return false
}

// printCliUpdatePrompt displays the CLI update notification.
func printCliUpdatePrompt(current, latest string) {
	fmt.Printf("%s %s → %s\n",
		blue("▶ New CLI version available:"),
		current,
		green(latest),
	)
	fmt.Printf("  %s\n", info("Run 'valet self-upgrade' to upgrade anytime."))
	fmt.Print("  Update CLI now? [y/N] ")
}

// printAnsibleUpdatePrompt displays the Ansible playbook update notification.
func printAnsibleUpdatePrompt() {
	fmt.Printf("%s\n", blue("▶ valet-sh playbook updates are available"))
	fmt.Printf("  %s\n", info("Run 'valet self-upgrade' to upgrade anytime."))
	fmt.Print("  Update playbooks now? [y/N] ")
}

// askYesNo reads a single line from stdin and returns true for "y" or "Y".
func askYesNo() bool {
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.TrimSpace(scanner.Text())
	return strings.EqualFold(answer, "y")
}

// reExec replaces the current process with a fresh invocation of the same
// binary and arguments, so the user's original command runs against the
// newly installed version without them having to retype it.
//
// Note: If Exec fails, we silently continue with the current process.
// This is acceptable because the update already succeeded; we just fall back
// to running the command with the old binary.
func reExec(args []string) {
	self, err := exec.LookPath(args[0])
	if err != nil {
		self = args[0]
	}
	// syscall.Exec replaces the process image — no return on success.
	_ = syscall.Exec(self, args, os.Environ())
}
