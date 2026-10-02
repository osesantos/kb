package tui

import (
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/osesantos/kb/internal/config"
	"github.com/osesantos/kb/internal/tui/components"
)

type formField struct{ label, text, hint string }

// form is a modal with labelled text fields; submit returns an error to keep it open and show the message.
type form struct {
	title  string
	fields []formField
	focus  int
	err    string
	submit func(m *Model, vals []string) error
}

func (f *form) values() []string {
	vals := make([]string, len(f.fields))
	for i, fl := range f.fields {
		vals[i] = strings.TrimSpace(fl.text)
	}
	return vals
}

func (m *Model) formKey(k string) tea.Cmd {
	f := m.form
	last := len(f.fields) - 1
	switch k {
	case "esc":
		m.form = nil
	case "tab", "down":
		f.focus = min(f.focus+1, last)
	case "shift+tab", "up":
		f.focus = max(f.focus-1, 0)
	case "enter":
		if f.focus < last {
			f.focus++
			return nil
		}
		if err := f.submit(m, f.values()); err != nil {
			f.err = err.Error()
		} else {
			m.form = nil
		}
	case "backspace":
		if r := []rune(f.fields[f.focus].text); len(r) > 0 {
			f.fields[f.focus].text = string(r[:len(r)-1])
		}
	case "ctrl+u":
		f.fields[f.focus].text = ""
	case "space":
		f.fields[f.focus].text += " "
	default:
		if r := []rune(k); len(r) == 1 {
			f.fields[f.focus].text += k
		}
	}
	return nil
}

// formPaste inserts pasted text into the focused field, flattening newlines.
func (m *Model) formPaste(s string) {
	s = strings.NewReplacer("\r", "", "\n", " ").Replace(s)
	m.form.fields[m.form.focus].text += s
}

func (m *Model) formView() string {
	f := m.form
	st := m.st.Form
	width := max(min(m.w-12, 64), 30)
	lines := []string{st.Title.Render(f.title), st.Title.Render(" ")}
	for i, fl := range f.fields {
		label, cursor := st.Label, ""
		if i == f.focus {
			label, cursor = st.LabelFocused, "█"
		}
		lines = append(lines, label.Render(fl.label), st.Input.Render(truncateTail(fl.text+cursor, width)))
		if fl.hint != "" {
			lines = append(lines, st.Hint.Render(fl.hint))
		}
		lines = append(lines, st.Input.Render(" "))
	}
	if f.err != "" {
		lines = append(lines, st.Error.Render(f.err), st.Input.Render(" "))
	}
	lines = append(lines, st.Hint.Render("tab next · ⏎ submit · esc cancel"))
	for i, l := range lines {
		lines[i] = lipgloss.NewStyle().Background(m.st.Form.Input.GetBackground()).Width(width).Render(l)
	}
	return components.Modal(m.st, strings.Join(lines, "\n"), width, 0)
}

// truncateTail keeps the end of s so the cursor stays visible in a long field.
func truncateTail(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return "…" + string(r[len(r)-w+1:])
}

// newVaultForm asks for a name and a path, validates both and appends the vault to the config file.
func newVaultForm(m *Model) *form {
	return &form{
		title: "New Vault",
		fields: []formField{
			{label: "Name", hint: "empty = folder name"},
			{label: "Path", text: "~/", hint: "an existing directory of Markdown notes"},
		},
		submit: func(m *Model, v []string) error {
			name, raw := v[0], v[1]
			real := config.Expand(raw)
			if st, err := os.Stat(real); err != nil || !st.IsDir() {
				return &formError{"not a directory: " + raw}
			}
			if name == "" {
				name = filepath.Base(real)
			}
			for _, c := range m.cfg.Vaults {
				if c.Name == name {
					return &formError{"a vault named " + name + " already exists"}
				}
				if c.Path == real {
					return &formError{"that path is already vault " + c.Name}
				}
			}
			if err := config.AddVault(m.cfgPath, name, raw); err != nil {
				return err
			}
			m.cfg.Vaults = append(m.cfg.Vaults, config.VaultCfg{Name: name, Path: real})
			if vv, ok := m.view.(*vaultsView); ok {
				vv.selIdx = len(m.cfg.Vaults) - 1
			}
			m.info("added vault %s — ⏎ to switch", name)
			return nil
		},
	}
}

type formError struct{ msg string }

func (e *formError) Error() string { return e.msg }
