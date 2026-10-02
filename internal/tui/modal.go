// Generic modal support: when a modal is open it owns the keyboard and the
// whole screen renders as a centered, bordered box on a blank canvas.
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/QuietSugar/resumer/internal/config"
	"github.com/QuietSugar/resumer/internal/provider"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalHelp
	modalProviders
	modalDetail
)

var modalKeys = []struct{ key, action string }{
	{"↑↓ / k j", "move"},
	{"/", "filter sessions"},
	{"enter", "resume"},
	{"esc", "cancel / clear filter"},
	{"tab ⇄", "provider source"},
	{"w", "focus list ↔ sidebar"},
	{"space", "sidebar: filter ws / toggle agent"},
	{"g", "group ↔ flat list"},
	{"o / v", "toggle sidebar / preview"},
	{"v v", "full detail popup"},
	{"ctrl-r", "rescan"},
	{"P", "provider management"},
	{"?", "this help"},
}

// allProviderNames lists every known provider: the picker's own set first
// (enabled ones), then anything else the registry knows (covers disabled
// providers, which are not part of the picker's provider list).
func (m Model) allProviderNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, p := range m.providers {
		if !seen[p.Name()] {
			seen[p.Name()] = true
			names = append(names, p.Name())
		}
	}
	for _, p := range provider.All() {
		if !seen[p.Name()] {
			seen[p.Name()] = true
			names = append(names, p.Name())
		}
	}
	sort.Strings(names)
	return names
}

func (m Model) providerModalContent() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("PROVIDERS") +
		helpStyle.Render("  space toggle · enter save · esc cancel") + "\n")
	names := m.allProviderNames()
	for i, name := range names {
		check := "[x]" // enabled
		if m.provTemp[name] {
			check = "[ ]"
		}
		cursor := "  "
		if i == m.provCur {
			cursor = cursorStyle.Render("▌ ")
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, check, name))
	}
	b.WriteString(dimStyle.Render("\nspace toggles scanning; enter persists to the config file."))
	return b.String()
}

func (m Model) helpContent() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("KEYS") + "\n")
	for _, k := range modalKeys {
		b.WriteString(fmt.Sprintf("  %-10s %s\n", k.key, k.action))
	}
	b.WriteString("\n" + headerStyle.Render("TIPS") + "\n")
	src := ""
	if m.tabIdx > 0 && m.tabIdx < len(m.tabs) {
		src = m.tabs[m.tabIdx]
	}
	b.WriteString(tipsForSource(src))
	return b.String()
}

func (m Model) renderModal() string {
	var content string
	boxW := m.width / 2
	if boxW < 46 {
		boxW = 46
	}
	if boxW > 76 {
		boxW = 76
	}
	if boxW > m.width-4 {
		boxW = m.width - 4
	}
	switch m.modal {
	case modalHelp:
		content = m.helpContent()
	case modalProviders:
		content = m.providerModalContent()
	case modalDetail:
		content = m.detail.View() + "\n" +
			dimStyle.Render("↑↓ scroll · v / esc close")
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1, 2).
		Width(boxW).
		Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// updateModal routes keys to the open modal. handled=false means the key was
// not consumed (only possible when no modal is open).
func (m *Model) updateModal(msg tea.Msg) (tea.Cmd, bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}
	switch m.modal {
	case modalHelp:
		switch key.String() {
		case "esc", "?", "q", "enter":
			m.modal = modalNone
		}
		return nil, true // swallow everything while open

	case modalDetail:
		switch key.String() {
		case "esc", "q", "v", "enter":
			m.modal = modalNone
		case "up", "k":
			m.detail.LineUp(1)
		case "down", "j":
			m.detail.LineDown(1)
		case "pgup", "ctrl+u", "b":
			m.detail.HalfPageUp()
		case "pgdown", "ctrl+d", "f":
			m.detail.HalfPageDown()
		case "home":
			m.detail.GotoTop()
		case "end", "G":
			m.detail.GotoBottom()
		}
		return nil, true // swallow

	case modalProviders:
		names := m.allProviderNames()
		switch key.String() {
		case "esc":
			m.modal = modalNone
		case "up", "k":
			if m.provCur > 0 {
				m.provCur--
			}
		case "down", "j":
			if m.provCur < len(names)-1 {
				m.provCur++
			}
		case " ":
			if m.provCur < len(names) {
				name := names[m.provCur]
				m.provTemp[name] = !m.provTemp[name]
			}
		case "enter":
			m.applyProviderModal()
			m.modal = modalNone
			return m.rescanCmds(), true
		}
		return nil, true // swallow
	}
	return nil, false
}

// applyProviderModal persists the temporary selection and applies it live.
func (m *Model) applyProviderModal() {
	var disabled []string
	for name, off := range m.provTemp {
		if off {
			disabled = append(disabled, name)
		}
	}
	sort.Strings(disabled)
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Config{}
	}
	cfg.Disabled = disabled
	if err := config.Save(cfg); err != nil {
		m.warnings = append(m.warnings, "warning: could not save config: "+err.Error())
		return
	}
	provider.SetDisabled(cfg.Disabled)
	m.syncProviders()
}
