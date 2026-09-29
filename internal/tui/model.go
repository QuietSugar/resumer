// Package tui is the native session picker (bubbletea), replacing the old
// fzf integration. Layout mirrors the fzf setup: session list on top,
// detail-box preview pane below.
//
// The picker never execs: it quits with a selection recorded, and the CLI
// performs the exec after the terminal is restored.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jin-ttao/resumer/internal/provider"
	"github.com/jin-ttao/resumer/internal/render"
	"github.com/jin-ttao/resumer/internal/session"
)

const helpLine = "↑↓ browse · / filter · tab source · ctrl-s sort · ctrl-r rescan · enter resume · esc cancel"

var (
	headerStyle  = lipgloss.NewStyle().Bold(true)
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	previewStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(lipgloss.Color("8"))
)

type loadedMsg struct {
	generation int
	name       string
	sessions   []session.Session
	err        error
}

// Model is the picker's bubbletea model. Exported for teatest.
type Model struct {
	filters   session.Filters
	providers []provider.Provider

	list    list.Model
	preview viewport.Model
	spin    spinner.Model

	all        []session.Session
	sources    []string // "" = all, else provider name
	sourceIdx  int
	sortAsc    bool
	pending    int
	generation int
	loaded     bool
	warnings   []string

	selected   *session.Session
	noSessions bool
	width      int
	height     int
	ready      bool
}

// NewModel builds the picker over the given providers (already filtered to
// active / requested-source ones by the caller).
func NewModel(providers []provider.Provider, filters session.Filters) Model {
	l := list.New(nil, rowDelegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	sources := []string{""}
	for _, p := range providers {
		sources = append(sources, p.Name())
	}

	return Model{
		filters:   filters,
		providers: providers,
		list:      l,
		preview:   viewport.New(0, 0),
		spin:      sp,
		sources:   sources,
		pending:   len(providers),
	}
}

// Selected returns the chosen session (nil on cancel).
func (m Model) Selected() *session.Session { return m.selected }

// NoSessions reports whether loading finished with zero sessions.
func (m Model) NoSessions() bool { return m.noSessions }

func (m Model) scanCmds() []tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.providers))
	for _, p := range m.providers {
		p := p
		f, generation := m.filters, m.generation
		cmds = append(cmds, func() tea.Msg {
			ss, err := p.ListSessions(f)
			return loadedMsg{generation: generation, name: p.Name(), sessions: ss, err: err}
		})
	}
	return cmds
}

func (m Model) Init() tea.Cmd {
	cmds := append([]tea.Cmd{m.spin.Tick}, m.scanCmds()...)
	return tea.Batch(cmds...)
}

func (m *Model) currentSessions() []session.Session {
	src := m.sources[m.sourceIdx]
	var out []session.Session
	for _, s := range m.all {
		if src == "" || s.Source == src {
			out = append(out, s)
		}
	}
	provider.SortSessions(out, m.sortAsc)
	return out
}

func (m *Model) applyItems() {
	selectedID, selectedSource := "", ""
	if selected, ok := m.list.SelectedItem().(sessionItem); ok {
		selectedID, selectedSource = selected.s.SessionID, selected.s.Source
	}

	sessions := m.currentSessions()
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		items[i] = sessionItem{s: s}
	}
	m.list.SetItems(items)
	selected := false
	for i, s := range sessions {
		if s.SessionID == selectedID && s.Source == selectedSource {
			m.list.Select(i)
			selected = true
			break
		}
	}
	if !selected && len(items) > 0 {
		m.list.Select(0)
	}
	m.refreshPreview()
}

func (m *Model) refreshPreview() {
	it, ok := m.list.SelectedItem().(sessionItem)
	if !ok {
		m.preview.SetContent("")
		return
	}
	m.preview.SetContent(render.FullBox(&it.s))
	m.preview.GotoTop()
}

func (m *Model) resize() {
	if m.width == 0 || m.height == 0 {
		return
	}
	headerH := 2 // title line + help line
	warnH := 0
	if len(m.warnings) > 0 {
		warnH = len(m.warnings)
	}
	bodyH := m.height - headerH - warnH
	if bodyH < 4 {
		bodyH = 4
	}
	listH := bodyH * 35 / 100
	if listH < 3 {
		listH = 3
	}
	previewH := bodyH - listH - 1 // border line
	if previewH < 3 {
		previewH = 3
	}
	m.list.SetSize(m.width, listH)
	m.preview.Width = m.width
	m.preview.Height = previewH
	m.ready = true
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil

	case loadedMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		if m.pending > 0 {
			m.pending--
		}
		if msg.err != nil {
			m.warnings = append(m.warnings,
				fmt.Sprintf("warning: %s provider failed: %v", msg.name, msg.err))
			m.resize()
		} else {
			kept := m.all[:0]
			for _, existing := range m.all {
				if existing.Source != msg.name {
					kept = append(kept, existing)
				}
			}
			m.all = append(kept, msg.sessions...)
			m.applyItems()
		}
		if m.pending == 0 {
			firstLoad := !m.loaded
			m.loaded = true
			if firstLoad && len(m.all) == 0 {
				m.noSessions = true
				return m, tea.Quit
			}
		}
		return m, nil

	case spinner.TickMsg:
		if m.pending > 0 {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+r" {
			m.generation++
			m.pending = len(m.providers)
			m.warnings = nil
			m.resize()
			cmds := append([]tea.Cmd{m.spin.Tick}, m.scanCmds()...)
			return m, tea.Batch(cmds...)
		}
		// While the user is typing a filter, the list owns the keyboard.
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch msg.String() {
		case "enter":
			if it, ok := m.list.SelectedItem().(sessionItem); ok {
				s := it.s
				m.selected = &s
				return m, tea.Quit
			}
			return m, nil
		case "esc":
			if m.list.FilterState() == list.FilterApplied {
				break // let the list clear its filter
			}
			return m, tea.Quit
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+s":
			m.sortAsc = !m.sortAsc
			m.applyItems()
			return m, nil
		case "tab":
			if len(m.sources) > 1 {
				m.sourceIdx = (m.sourceIdx + 1) % len(m.sources)
				m.applyItems()
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	prevIndex := m.list.Index()
	m.list, cmd = m.list.Update(msg)
	if m.list.Index() != prevIndex || m.list.SettingFilter() || m.list.FilterState() != list.Unfiltered {
		m.refreshPreview()
	}
	return m, cmd
}

func (m Model) View() string {
	if !m.ready {
		return "loading…"
	}
	var b strings.Builder

	title := "resumer"
	src := m.sources[m.sourceIdx]
	if src == "" {
		src = "all"
	}
	status := fmt.Sprintf("%d sessions · source: %s", len(m.list.Items()), src)
	if m.pending > 0 {
		status = m.spin.View() + " loading · " + status
	}
	b.WriteString(headerStyle.Render(title) + "  " + helpStyle.Render(status) + "\n")
	b.WriteString(helpStyle.Render(helpLine) + "\n")
	for _, w := range m.warnings {
		b.WriteString(warnStyle.Render(w) + "\n")
	}
	b.WriteString(m.list.View() + "\n")
	b.WriteString(previewStyle.Width(m.width).Render(m.preview.View()))
	return b.String()
}

// Pick runs the picker. Returns (selection, sawNoSessions, error); selection
// is nil on cancel.
func Pick(filters session.Filters) (*session.Session, bool, error) {
	var providers []provider.Provider
	if filters.Source != "" {
		p := provider.Get(filters.Source)
		if p == nil {
			return nil, false, fmt.Errorf("unknown provider: %s", filters.Source)
		}
		if !p.IsAvailable() {
			return nil, false, fmt.Errorf(
				"%s provider not available (binary or session directory missing)", filters.Source)
		}
		providers = []provider.Provider{p}
	} else {
		providers = provider.Active()
	}

	prog := tea.NewProgram(NewModel(providers, filters), tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		return nil, false, err
	}
	m, ok := final.(Model)
	if !ok {
		return nil, false, fmt.Errorf("unexpected model type")
	}
	return m.Selected(), m.NoSessions(), nil
}
