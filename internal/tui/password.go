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
)

// PasswordBox is a masked single-line input shown before executing a command
// from the TUI launcher. It collects the sudo/become password for Ansible.
//
// Security properties:
//   - The password is stored only in the bubbles/textinput internal buffer
//     while the user is typing.
//   - TakePassword() moves it to a []byte, clears the input, and returns the
//     value. The caller (launcher.go) passes it to ansible.RunSubprocess()
//     which zeros it immediately after writing it to the subprocess env.
//   - No caching between commands — PasswordBox is discarded after each use.
type PasswordBox struct {
	// input is the masked textinput.
	input textinput.Model

	// command is shown in the prompt context line above the input.
	command string

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

// View renders the password box as a bordered string.
func (b PasswordBox) View() string {
	innerWidth := b.width - 4

	contextLine := styles.GhostCommand.Render("valet.sh " + b.command)
	hintLine := styles.HelpDesc.Render("Required for privileged operations.")

	content := contextLine + "\n" +
		b.input.View() + "\n\n" +
		hintLine

	return styles.PreviewBox.Width(innerWidth).Render(content)
}
