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
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// ArgPane manages the argument input fields shown at the bottom of the TUI
// when a command with required/optional arguments is selected.
type ArgPane struct {
	defs   []ArgDef
	inputs []textinput.Model
	focus  int
	width  int
}

// NewArgPane creates an ArgPane for the given argument definitions.
func NewArgPane(defs []ArgDef, width int) ArgPane {
	inputs := make([]textinput.Model, len(defs))
	for i, def := range defs {
		t := textinput.New()
		t.Placeholder = def.Name
		if def.Required {
			t.Placeholder = def.Name + " (required)"
		} else {
			t.Placeholder = def.Name + " (optional)"
		}
		t.CharLimit = 256
		t.SetWidth(width/2 - 4)
		inputs[i] = t
	}

	p := ArgPane{
		defs:   defs,
		inputs: inputs,
		focus:  0,
		width:  width,
	}
	p.focusField(0)
	return p
}

// IsEmpty returns true when there are no argument definitions.
func (p ArgPane) IsEmpty() bool { return len(p.defs) == 0 }

// IsReady returns true when all required fields are filled.
func (p ArgPane) IsReady() bool {
	for i, def := range p.defs {
		if def.Required && strings.TrimSpace(p.inputs[i].Value()) == "" {
			return false
		}
	}
	return true
}

// Values returns the current input values in order.
func (p ArgPane) Values() []string {
	vals := make([]string, 0, len(p.inputs))
	for i := range p.inputs {
		v := strings.TrimSpace(p.inputs[i].Value())
		if v != "" {
			vals = append(vals, v)
		}
	}
	return vals
}

// Update passes tea.Msg to the focused input field.
func (p ArgPane) Update(msg tea.Msg) (ArgPane, tea.Cmd) {
	if len(p.inputs) == 0 {
		return p, nil
	}

	var cmds []tea.Cmd

	if keyMsg, ok := msg.(tea.KeyPressMsg); ok {
		switch keyMsg.String() {
		case "tab", "down":
			p.focusField((p.focus + 1) % len(p.inputs))
			return p, nil
		case "shift+tab", "up":
			p.focusField((p.focus - 1 + len(p.inputs)) % len(p.inputs))
			return p, nil
		}
	}

	// Route keystrokes to the focused field.
	var cmd tea.Cmd
	p.inputs[p.focus], cmd = p.inputs[p.focus].Update(msg)
	cmds = append(cmds, cmd)

	return p, tea.Batch(cmds...)
}

// View renders the argument pane as a string.
func (p ArgPane) View() string {
	if len(p.defs) == 0 {
		return ""
	}

	var sb strings.Builder

	_, _ = fmt.Fprintln(&sb, styles.ArgPaneTitle.Render("▶ Arguments"))

	for i, def := range p.defs {
		req := ""
		if def.Required {
			req = styles.HelpKey.Render("*")
		}

		label := fmt.Sprintf("  %s%s  ", styles.ArgLabel.Render(def.Name), req)

		var inputView string
		if i == p.focus {
			inputView = styles.ArgInputFocus.Render(p.inputs[i].View())
		} else {
			inputView = styles.ArgInput.Render(p.inputs[i].View())
		}

		_, _ = fmt.Fprintln(&sb, label+inputView)
	}

	hint := "  "
	if p.IsReady() {
		hint += styles.HelpKey.Render("↵ run") + "  " + styles.HelpDesc.Render("tab next field")
	} else {
		hint += styles.HelpDesc.Render("* required  tab next field")
	}
	_, _ = fmt.Fprintln(&sb, hint)

	return sb.String()
}

// focusField updates the focus state so only the given field is active.
func (p *ArgPane) focusField(idx int) {
	for i := range p.inputs {
		if i == idx {
			p.inputs[i].Focus()
		} else {
			p.inputs[i].Blur()
		}
	}
	p.focus = idx
}
