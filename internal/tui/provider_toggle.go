// Interactive provider enable/disable screen — a checkbox list driven by the
// same bubbletea runtime as the picker, so enabling/disabling needs no new
// dependencies. `resumer provider` (bare) opens it; space toggles, enter
// saves, esc leaves the config untouched.
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	toggleTitleStyle = lipgloss.NewStyle().Bold(true)
	// toggleHintStyle/toggleNoteStyle reuse the picker's dim + amber styles.
	toggleNoteStyle = warnStyle
)

type toggleRow struct {
	name string
	off  bool
	note string
}

type toggleModel struct {
	rows     []toggleRow
	cursor   int
	canceled bool
}

func (m *toggleModel) toggleCurrent() {
	if len(m.rows) > 0 {
		m.rows[m.cursor].off = !m.rows[m.cursor].off
	}
}

// disabledSet returns the names currently toggled off, in row order.
func (m toggleModel) disabledSet() []string {
	var out []string
	for _, r := range m.rows {
		if r.off {
			out = append(out, r.name)
		}
	}
	return out
}

func (m toggleModel) Init() tea.Cmd { return nil }

func (m toggleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case " ", "x", "space":
			m.toggleCurrent()
		case "enter":
			return m, tea.Quit
		case "esc", "q", "ctrl+c":
			m.canceled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m toggleModel) View() string {
	var b strings.Builder
	b.WriteString(toggleTitleStyle.Render("resumer providers"))
	b.WriteString("  " + helpStyle.Render("space toggle · enter save · esc cancel") + "\n")
	for i, r := range m.rows {
		cursor, box := "  ", "[*] "
		if r.off {
			box = "[ ] "
		}
		if i == m.cursor {
			cursor = "› "
		}
		line := fmt.Sprintf("%s%s%s", cursor, box, r.name)
		if r.note != "" {
			line += "  " + toggleNoteStyle.Render(r.note)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// ProviderToggle runs the interactive checkbox screen. names lists the known
// providers in display order; disabled is the current off-set (entries not
// present in names are still shown, annotated "(unknown)", so configs
// outliving a provider remain visible and clearable); storage reports
// whether each provider's session storage was detected. It returns the
// resulting disabled list in row order; canceled reports an esc exit with no
// changes applied.
func ProviderToggle(names []string, disabled map[string]bool, storage map[string]bool) ([]string, bool, error) {
	var rows []toggleRow
	for _, n := range names {
		note := ""
		if s, known := storage[n]; known && !s {
			note = "storage missing"
		}
		rows = append(rows, toggleRow{name: n, off: disabled[n], note: note})
	}
	var extras []string
	for n := range disabled {
		known := false
		for _, k := range names {
			if k == n {
				known = true
				break
			}
		}
		if !known {
			extras = append(extras, n)
		}
	}
	sort.Strings(extras)
	for _, n := range extras {
		rows = append(rows, toggleRow{name: n, off: true, note: "(unknown)"})
	}

	final, err := tea.NewProgram(toggleModel{rows: rows}).Run()
	if err != nil {
		return nil, false, err
	}
	tm, ok := final.(toggleModel)
	if !ok {
		return nil, false, fmt.Errorf("unexpected model type")
	}
	if tm.canceled {
		return nil, true, nil
	}
	return tm.disabledSet(), false, nil
}
