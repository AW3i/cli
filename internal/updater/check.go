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

// Package updater performs a periodic background check for new valet-sh
// releases and prompts the user to update when one is available.
package updater

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// checkInterval is how often the GitHub API is consulted.
	checkInterval = 7 * 24 * time.Hour

	// timestampFile records the last time the check ran.
	timestampFile = "/usr/local/valet-sh/etc/.last_update_check"

	// githubReleaseURL is the GitHub API endpoint for the latest release.
	githubReleaseURL = "https://api.github.com/repos/valet-sh/valet-sh/releases/latest"

	// apiTimeout caps the HTTP call so a slow network never blocks the user.
	apiTimeout = 3 * time.Second

	// installerBin is the update command.
	installerBin = "/usr/local/valet-sh/installer/valet-sh-installer"
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
}

// Check runs the periodic update check. It is a no-op when:
//   - the check was run less than checkInterval ago
//   - the GitHub API is unreachable (fails silently)
//   - currentVersion is "dev" (local development build)
//
// originalArgs is os.Args so the command can be re-executed after updating.
func Check(currentVersion string, originalArgs []string) {
	if currentVersion == "dev" {
		return
	}

	if !checkDue() {
		return
	}

	// Always write the timestamp first so a network error doesn't cause
	// the check to hammer the API on every subsequent invocation.
	writeTimestamp()

	latest, err := fetchLatestTag()
	if err != nil {
		// Silent — network issues should never interrupt normal usage.
		return
	}

	if !isNewer(latest, currentVersion) {
		return
	}

	printUpdatePrompt(currentVersion, latest)

	if !askYesNo() {
		fmt.Println(info(fmt.Sprintf("Skipping. Run 'valet-sh-installer update' to upgrade to %s anytime.", green(latest))))
		fmt.Println()
		return
	}

	fmt.Println()
	runUpdate()

	// Re-exec the original command so the user doesn't have to retype it.
	reExec(originalArgs)
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
	f, err := os.OpenFile(timestampFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	f.Close()
}

// fetchLatestTag queries the GitHub releases API and returns the tag name.
func fetchLatestTag() (string, error) {
	client := &http.Client{Timeout: apiTimeout}
	req, err := http.NewRequest(http.MethodGet, githubReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var rel releaseResponse
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}

	return strings.TrimPrefix(rel.TagName, "v"), nil
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

// printUpdatePrompt displays the styled update notification.
func printUpdatePrompt(current, latest string) {
	fmt.Println()
	fmt.Printf("%s %s → %s\n",
		blue("▶ New version available:"),
		current,
		green(latest),
	)
	fmt.Printf("  %s\n\n", info("Run 'valet-sh-installer update' to upgrade, or answer below."))
	fmt.Print("  Update now? [y/N] ")
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

// runUpdate executes valet-sh-installer update, streaming output to the
// terminal. On failure it prints a warning but does not abort.
func runUpdate() {
	cmd := exec.Command(installerBin, "update")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s update failed: %v\n", ansiRed+"✘"+ansiReset, err)
	}
}

// reExec replaces the current process with a fresh invocation of the same
// binary and arguments, so the user's original command runs against the
// newly installed version without them having to retype it.
func reExec(args []string) {
	self, err := exec.LookPath(args[0])
	if err != nil {
		self = args[0]
	}
	// syscall.Exec replaces the process image — no return on success.
	_ = syscall.Exec(self, args, os.Environ())
}
