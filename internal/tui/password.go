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

package tui

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// PasswordBox is a masked single-line input shown before executing a command
// from the TUI launcher. It collects the sudo/become password for Ansible.
//
// Security properties:
//   - The password is stored only in the bubbles/textinput internal buffer
//     while the user is typing.
//   - TakePassword() moves it to a []byte, clears the input, and returns the
//     value. The caller (launcher.go) passes it to ansible.RunSubprocess()
//     which zeros it immediately after writing it to ANSIBLE_BECOME_PASS env var.
//   - No caching between commands — PasswordBox is discarded after each use.
//   - stdin is NOT used — Bubble Tea owns stdin during TUI execution.
type PasswordBox struct {
	// input is the masked textinput.
	input textinput.Model

	// command is shown in the prompt context line above the input.
	command string

	// showEmptyError is true when the user pressed Enter on an empty field.
	showEmptyError bool

	// width is the total width available for the box.
	width int
}

// NewPasswordBox creates a PasswordBox for the given command context.
// command is e.g. "service start php83" — shown in the context line.
func NewPasswordBox(command string, width int) PasswordBox {
	inp := textinput.New()
	inp.Placeholder = "sudo password"
	inp.EchoMode = textinput.EchoPassword
	inp.CharLimit = 256

	innerWidth := width - 4 // 2 border + 2 padding
	inp.SetWidth(innerWidth)

	inp.Focus()

	// Style the prompt.
	inputStyles := inp.Styles()
	inputStyles.Focused.Prompt = styles.InputGhostPrompt
	inputStyles.Focused.Text = styles.InputText
	inp.SetStyles(inputStyles)

	return PasswordBox{
		input:   inp,
		command: command,
		width:   width,
	}
}

// TakePassword returns the current password as a []byte and immediately
// clears the input field so the value does not persist in the textinput.
// The caller is responsible for zeroing the returned slice after use.
func (b *PasswordBox) TakePassword() []byte {
	val := []byte(b.input.Value())
	b.input.SetValue("")
	return val
}

// InputView returns the rendered textinput for embedding in the main content area.
// The input field itself is rendered directly in the screen, not inside a box.
func (b PasswordBox) InputView() string {
	return b.input.View()
}

// IsEmpty returns true when no password has been typed yet.
func (b PasswordBox) IsEmpty() bool {
	return strings.TrimSpace(b.input.Value()) == ""
}

// Update handles key events for the password input.
func (b PasswordBox) Update(msg tea.Msg) (PasswordBox, tea.Cmd) {
	var cmd tea.Cmd
	b.input, cmd = b.input.Update(msg)
	return b, cmd
}

// MarkEmptyError sets the error state shown when user presses Enter on an
// empty field, so they get clear feedback without accidentally submitting.
func (b *PasswordBox) MarkEmptyError() {
	b.showEmptyError = true
}

// View renders only the hint line (error or help text).
// The password input itself is rendered separately via InputView(),
// and the command is shown in the header, so there's no redundancy.
func (b PasswordBox) View() string {
	if b.showEmptyError {
		return lipgloss.NewStyle().Foreground(colourRed).Render("Password cannot be empty.")
	}
	return styles.HelpDesc.Render("Required for privileged operations.")
}
