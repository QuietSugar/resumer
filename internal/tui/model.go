// Package tui is the native session picker (bubbletea): a three-panel
// dashboard — top bar with provider tabs, collapsible workspace sidebar,
// full-height session list, and a detail panel.
//
// The picker never execs: it quits with a selection recorded, and the CLI
// performs the exec after the terminal is restored.
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"

	"github.com/QuietSugar/resumer/internal/config"
	"github.com/QuietSugar/resumer/internal/provider"
	"github.com/QuietSugar/resumer/internal/render"
	"github.com/QuietSugar/resumer/internal/session"
	"github.com/QuietSugar/resumer/internal/workspace"
)

type focus int

const (
	focusList focus = iota
	focusSidebar
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
	all       []session.Session

	tabs   []string // "" = all sources, then enabled provider names
	tabIdx int

	width, height int
	ready, sized  bool

	sidebarOn, previewOn bool
	focus                focus
	sideCur              int
	wsFilter             string // group key of the workspace the sidebar filters by

	grouped bool // group-mode rows (default: on; g toggles the flat list)

	cursor, offset int
	filtering      bool
	filterInput    textinput.Model
	filterValue    string

	preview viewport.Model
	detail  viewport.Model // full-detail popup content
	spin    spinner.Model

	lastV time.Time // double-tap detection for the full-detail popup

	modal    modalKind
	provCur  int
	provTemp map[string]bool

	pending, generation int
	loaded, noSessions  bool
	warnings            []string

	rows       []row
	selectable []int
	groups     []workspace.Group
	tabCounts  map[string]int
	workspaces int

	selKey   string
	selected *session.Session
}

// NewModel builds the picker over the given providers (already filtered to
// active / requested-source ones by the caller).
func NewModel(providers []provider.Provider, filters session.Filters) Model {
	ti := textinput.New()
	ti.Placeholder = "filter sessions…"
	ti.Prompt = "/"

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	m := Model{
		filters:     filters,
		providers:   providers,
		grouped:     true,
		tabs:        []string{""},
		filterInput: ti,
		preview:     viewport.New(0, 0),
		spin:        sp,
		provTemp:    map[string]bool{},
		pending:     len(providers),
	}
	for _, p := range providers {
		m.tabs = append(m.tabs, p.Name())
	}
	return m
}

// Selected returns the chosen session (nil on cancel).
func (m Model) Selected() *session.Session { return m.selected }

// NoSessions reports whether loading finished with zero sessions.
func (m Model) NoSessions() bool { return m.noSessions }

// --- data pipeline ---------------------------------------------------------

// tabSessions returns the sessions of the active provider tab (before any
// sidebar/text filtering).
func (m Model) tabSessions() []session.Session {
	src := ""
	if m.tabIdx > 0 && m.tabIdx < len(m.tabs) {
		src = m.tabs[m.tabIdx]
	}
	var out []session.Session
	for _, s := range m.all {
		if src == "" || s.Source == src {
			out = append(out, s)
		}
	}
	return out
}

// applyTextFilter narrows ss by the fuzzy filter value, preserving order.
func applyTextFilter(ss []session.Session, query string) []session.Session {
	query = strings.TrimSpace(query)
	if query == "" {
		return ss
	}
	targets := make([]string, len(ss))
	for i := range ss {
		targets[i] = filterValue(&ss[i])
	}
	matches := fuzzy.Find(query, targets)
	idx := make([]int, 0, len(matches))
	for _, mt := range matches {
		idx = append(idx, mt.Index)
	}
	sort.Ints(idx)
	out := make([]session.Session, 0, len(idx))
	for _, i := range idx {
		out = append(out, ss[i])
	}
	return out
}

// rebuild recomputes groups, rows, and the cursor from the current state.
// It is the single funnel every mutation goes through, so grouping, the
// sidebar, the tabs, and the selection can never disagree.
func (m *Model) rebuild() {
	base := m.tabSessions()
	m.groups = workspace.GroupBy(base)
	m.workspaces = len(m.groups)

	m.tabCounts = map[string]int{"": len(m.all)}
	for _, s := range m.all {
		m.tabCounts[s.Source]++
	}

	if m.wsFilter != "" {
		var f []session.Session
		for _, s := range base {
			if workspace.Key(&s) == m.wsFilter {
				f = append(f, s)
			}
		}
		base = f
	}
	base = applyTextFilter(base, m.filterValue)
	provider.SortSessions(base, false)

	m.rows = make([]row, 0, len(base)+1)
	m.selectable = m.selectable[:0]
	if m.grouped {
		for _, g := range workspace.GroupBy(base) {
			m.rows = append(m.rows, row{kind: rowGroup, g: g})
			for _, s := range g.Sessions {
				m.selectable = append(m.selectable, len(m.rows))
				m.rows = append(m.rows, row{kind: rowSession, s: s})
			}
		}
	} else {
		for _, s := range base {
			m.selectable = append(m.selectable, len(m.rows))
			m.rows = append(m.rows, row{kind: rowSession, s: s})
		}
	}
	m.restoreSelection()
	m.refreshPreview()
}

// restoreSelection puts the cursor back on the previously selected session
// (matched by identity), else on the first session row.
func (m *Model) restoreSelection() {
	if m.selKey != "" {
		for i, r := range m.rows {
			if r.kind == rowSession && sessionKey(&r.s) == m.selKey {
				m.cursor = i
				m.clampOffset()
				return
			}
		}
	}
	if len(m.selectable) > 0 {
		m.cursor = m.selectable[0]
	} else {
		m.cursor = 0
	}
	m.clampOffset()
}

func (m *Model) rememberSelection() {
	if m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowSession {
		m.selKey = sessionKey(&m.rows[m.cursor].s)
	}
}

func (m *Model) selectedSession() *session.Session {
	if m.cursor < len(m.rows) && m.rows[m.cursor].kind == rowSession {
		s := m.rows[m.cursor].s
		return &s
	}
	return nil
}

// --- navigation ------------------------------------------------------------

func (m Model) pageSize() int {
	h := m.listBodyHeight()
	if h < 1 {
		return 1
	}
	return h
}

// moveCursor steps the selection by delta session rows, skipping headers.
func (m *Model) moveCursor(delta int) {
	if len(m.selectable) == 0 {
		return
	}
	j := sort.SearchInts(m.selectable, m.cursor)
	exact := j < len(m.selectable) && m.selectable[j] == m.cursor
	var k int
	switch {
	case exact:
		k = j + delta
	case delta < 0:
		k = j - 1
	default:
		k = j
	}
	if k < 0 {
		k = 0
	}
	if k >= len(m.selectable) {
		k = len(m.selectable) - 1
	}
	m.cursor = m.selectable[k]
	m.rememberSelection()
	m.clampOffset()
	m.refreshPreview()
}

func (m *Model) jumpTo(end bool) {
	if len(m.selectable) == 0 {
		return
	}
	k := 0
	if end {
		k = len(m.selectable) - 1
	}
	m.cursor = m.selectable[k]
	m.rememberSelection()
	m.clampOffset()
	m.refreshPreview()
}

// clampOffset keeps the cursor inside the visible scroll window.
func (m *Model) clampOffset() {
	h := m.listBodyHeight()
	if h <= 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if max := len(m.rows) - h; m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *Model) refreshPreview() {
	s := m.selectedSession()
	if s == nil {
		m.preview.SetContent("")
		return
	}
	meta := append(styleMetaLines(render.MetaLines(s)),
		"", helpStyle.Render(fmt.Sprintf("prompts: %d  ·  vv conversation", len(s.Prompts))))
	m.preview.SetContent(strings.Join(wrapLines(meta, m.preview.Width), "\n"))
	m.preview.GotoTop()
}

// --- panels & layout -------------------------------------------------------

// panelWidths resolves the effective panel widths for the current width and
// toggle state, guaranteeing the list keeps workable room.
func (m Model) panelWidths() (sideW, prevW, listW int) {
	listW = m.width
	if m.sidebarOn && m.width >= 100 {
		sideW = 20
		listW -= sideW + 1 // + separator
	}
	if m.previewOn && listW >= 90 {
		prevW = listW * 2 / 5
		if prevW > 42 {
			prevW = 42
		}
		if prevW < 30 {
			prevW = 30
		}
		listW -= prevW + 1 // + separator
	}
	if listW < 30 && prevW > 0 {
		listW += prevW + 1
		prevW = 0
	}
	if listW < 30 && sideW > 0 {
		listW += sideW + 1
		sideW = 0
	}
	return sideW, prevW, listW
}

func (m Model) mainHeight() int {
	h := m.height - 2 // top + bottom bars
	if len(m.warnings) > 0 {
		h--
	}
	if m.filtering {
		h--
	}
	if h < 1 {
		h = 1
	}
	return h
}

func (m Model) listBodyHeight() int { return m.mainHeight() - 1 } // column header line

func (m *Model) resize() {
	if m.width == 0 || m.height == 0 {
		return
	}
	_, prevW, _ := m.panelWidths()
	m.preview.Width = prevW
	if m.preview.Width < 10 {
		m.preview.Width = 10
	}
	m.preview.Height = m.mainHeight() - 2
	if m.preview.Height < 3 {
		m.preview.Height = 3
	}
	m.ready = true
	m.clampOffset()
}

// --- provider / agent management -------------------------------------------

// syncProviders re-derives tabs and the session pool after a provider
// enable/disable. Outside tests it re-reads the registry so availability and
// the enabled set stay authoritative.
func (m *Model) syncProviders() {
	if m.filters.Source == "" && len(provider.All()) > 0 {
		m.providers = provider.Active()
	}
	seen := map[string]bool{}
	tabs := []string{""}
	enabled := map[string]bool{}
	for _, p := range m.providers {
		name := p.Name()
		if !provider.IsEnabled(name) || seen[name] {
			continue
		}
		seen[name] = true
		enabled[name] = true
		tabs = append(tabs, name)
	}
	m.tabs = tabs
	if m.tabIdx >= len(m.tabs) {
		m.tabIdx = 0
	}
	kept := m.all[:0]
	for _, s := range m.all {
		if enabled[s.Source] {
			kept = append(kept, s)
		}
	}
	m.all = kept
	m.rebuild()
}

// toggleAgent flips one provider's enabled state, persists it, and applies it
// live. Enabling schedules a rescan so the provider's sessions load.
func (m *Model) toggleAgent(name string) tea.Cmd {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Config{}
	}
	if cfg.IsDisabled(name) {
		cfg.Enable(name)
	} else {
		cfg.Disable(name)
	}
	if err := config.Save(cfg); err != nil {
		m.warnings = append(m.warnings, "warning: could not save config: "+err.Error())
		return nil
	}
	provider.SetDisabled(cfg.Disabled)

	had := false
	for _, s := range m.all {
		if s.Source == name {
			had = true
			break
		}
	}
	m.syncProviders()
	if !had && provider.Get(name) != nil && provider.IsEnabled(name) {
		return m.rescanCmds() // newly enabled — load its sessions
	}
	return nil
}

func (m *Model) openProviderModal() {
	m.modal = modalProviders
	m.provCur = 0
	m.provTemp = map[string]bool{}
	for _, name := range provider.DisabledNames() {
		m.provTemp[name] = true
	}
}

// openDetailModal shows the selected session's full detail (every prompt,
// long lines wrapped) in a scrollable popup.
func (m *Model) openDetailModal() {
	s := m.selectedSession()
	if s == nil {
		return
	}
	bw := m.width / 2
	if bw < 52 {
		bw = 52
	}
	if bw > 100 {
		bw = 100
	}
	if bw > m.width-6 {
		bw = m.width - 6
	}
	m.detail.Width = bw - 6
	h := m.height - 8
	if h < 6 {
		h = 6
	}
	m.detail.Height = h
	var body []string
	conv := render.ConversationLines(s)
	if len(conv) == 0 {
		body = []string{dimStyle.Render("(no prompts in this session)")}
	} else {
		body = wrapLines(conv, m.detail.Width)
	}
	header := cut(s.Title, m.detail.Width)
	m.detail.SetContent(strings.Join(append([]string{headerStyle.Render(header), ""}, body...), "\n"))
	m.detail.GotoTop()
	m.modal = modalDetail
}

func (m *Model) rescanCmds() tea.Cmd {
	m.generation++
	m.pending = len(m.providers)
	m.warnings = nil
	cmds := append([]tea.Cmd{m.spin.Tick}, m.scanCmds()...)
	return tea.Batch(cmds...)
}

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

// --- bubbletea plumbing ----------------------------------------------------

func (m Model) Init() tea.Cmd {
	cmds := append([]tea.Cmd{m.spin.Tick}, m.scanCmds()...)
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.sized {
			m.sized = true
			m.sidebarOn = m.width >= 140
			m.previewOn = m.width >= 100
		}
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
		} else {
			kept := m.all[:0]
			for _, existing := range m.all {
				if existing.Source != msg.name {
					kept = append(kept, existing)
				}
			}
			m.all = append(kept, msg.sessions...)
			m.rebuild()
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
		if m.modal != modalNone {
			cmd, _ := m.updateModal(msg)
			return m, cmd
		}
		if msg.String() == "ctrl+r" {
			m.generation++
			m.pending = len(m.providers)
			m.warnings = nil
			cmds := append([]tea.Cmd{m.spin.Tick}, m.scanCmds()...)
			return m, tea.Batch(cmds...)
		}
		if m.filtering {
			return m.updateFilter(msg)
		}
		if m.focus == focusSidebar {
			return m.updateSidebar(msg)
		}
		switch msg.String() {
		case "enter":
			if s := m.selectedSession(); s != nil {
				sel := s
				m.selected = sel
				return m, tea.Quit
			}
			return m, nil
		case "esc":
			switch {
			case m.filterValue != "":
				m.filterValue = ""
				m.filterInput.SetValue("")
				m.rebuild()
				return m, nil
			case m.wsFilter != "":
				m.wsFilter = ""
				m.rebuild()
				return m, nil
			}
			return m, tea.Quit
		case "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.switchTab(1)
			return m, nil
		case "shift+tab", "left":
			m.switchTab(-1)
			return m, nil
		case "right":
			m.switchTab(1)
			return m, nil
		case "/":
			m.filtering = true
			m.filterInput.Focus()
			m.filterInput.CursorEnd()
			return m, textinput.Blink
		case "w":
			if m.sidebarOn && m.width >= 100 {
				m.focus = focusSidebar
			}
			return m, nil
		case "o":
			m.sidebarOn = !m.sidebarOn
			m.resize()
			return m, nil
		case "v":
			now := time.Now()
			if now.Sub(m.lastV) <= 600*time.Millisecond {
				m.lastV = time.Time{}
				m.openDetailModal()
				return m, nil
			}
			m.lastV = now
			m.previewOn = !m.previewOn
			m.resize()
			return m, nil
		case "g":
			m.grouped = !m.grouped
			m.rebuild()
			return m, nil
		case "P":
			m.openProviderModal()
			return m, nil
		case "?":
			m.modal = modalHelp
			return m, nil
		case "up", "k":
			m.moveCursor(-1)
			return m, nil
		case "down", "j":
			m.moveCursor(1)
			return m, nil
		case "pgup", "b":
			m.moveCursor(-m.pageSize())
			return m, nil
		case "pgdown", "f":
			m.moveCursor(m.pageSize())
			return m, nil
		case "home":
			m.jumpTo(false)
			return m, nil
		case "end", "G":
			m.jumpTo(true)
			return m, nil
		case "ctrl+d":
			m.preview.HalfPageDown()
			return m, nil
		case "ctrl+u":
			m.preview.HalfPageUp()
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) switchTab(delta int) {
	if len(m.tabs) > 1 {
		m.tabIdx = (m.tabIdx + delta + len(m.tabs)) % len(m.tabs)
	}
	m.rebuild()
}

func (m Model) updateFilter(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if ok {
		switch key.String() {
		case "enter":
			m.filtering = false
			m.filterInput.Blur()
			return m, nil
		case "esc":
			m.filtering = false
			m.filterValue = ""
			m.filterInput.SetValue("")
			m.filterInput.Blur()
			m.rebuild()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	if m.filterInput.Value() != m.filterValue {
		m.filterValue = m.filterInput.Value()
		m.rebuild()
	}
	return m, cmd
}

func (m Model) updateSidebar(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	items := len(m.groups) + len(m.providers)
	switch key.String() {
	case "esc", "w":
		m.focus = focusList
		return m, nil
	case "up", "k":
		if m.sideCur > 0 {
			m.sideCur--
		}
		return m, nil
	case "down", "j":
		if m.sideCur < items-1 {
			m.sideCur++
		}
		return m, nil
	case " ", "enter":
		return m, m.activateSidebarItem()
	}
	return m, nil
}

// activateSidebarItem applies the item under the sidebar cursor: workspace
// items toggle the workspace filter, agent items toggle the provider.
func (m *Model) activateSidebarItem() tea.Cmd {
	if m.sideCur < len(m.groups) {
		g := m.groups[m.sideCur]
		if m.wsFilter == g.Key {
			m.wsFilter = ""
		} else {
			m.wsFilter = g.Key
		}
		m.rebuild()
		return nil
	}
	j := m.sideCur - len(m.groups)
	if j >= 0 && j < len(m.providers) {
		return m.toggleAgent(m.providers[j].Name())
	}
	return nil
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
