package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/QuietSugar/resumer/internal/provider"
	"github.com/QuietSugar/resumer/internal/session"
	"github.com/QuietSugar/resumer/internal/textutil"
)

type refreshProvider struct {
	calls int
}

func (p *refreshProvider) Name() string      { return "test" }
func (p *refreshProvider) Badge() string     { return "test" }
func (p *refreshProvider) BadgeANSI() string { return "test" }
func (p *refreshProvider) IsAvailable() bool { return true }
func (p *refreshProvider) LoadDetail(string) (*session.Session, error) {
	return nil, nil
}
func (p *refreshProvider) ListSessions(session.Filters) ([]session.Session, error) {
	p.calls++
	title := "Updated title"
	if p.calls == 1 {
		title = "Original title"
	}
	return []session.Session{{
		Source: "test", SessionID: "session-1", Title: title, FirstPrompt: "hello",
		LastTS: "2026-09-29T10:00:00Z",
	}}, nil
}

func TestCtrlRRescansAndUpdatesSessionMetadata(t *testing.T) {
	p := &refreshProvider{}
	m := NewModel([]provider.Provider{p}, session.Filters{})
	// Deliver the initial scan result directly to keep the test deterministic.
	initial := m.scanCmds()[0]()
	updated, _ := m.Update(initial)
	m = updated.(Model)
	if p.calls != 1 {
		t.Fatalf("initial scan calls = %d, want 1", p.calls)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = updated.(Model)
	if m.pending != 1 || m.generation != 1 {
		t.Fatalf("after rescan: pending=%d generation=%d, want 1 and 1", m.pending, m.generation)
	}

	refreshed := m.scanCmds()[0]()
	updated, _ = m.Update(refreshed)
	m = updated.(Model)
	if p.calls != 2 {
		t.Fatalf("scan calls = %d, want 2", p.calls)
	}
	item, ok := m.list.SelectedItem().(sessionItem)
	if !ok {
		t.Fatal("expected selected session after rescan")
	}
	if item.s.Title != "Updated title" {
		t.Fatalf("title = %q, want updated metadata", item.s.Title)
	}
}

func TestRowDelegateRendersTitleWithoutFirstPrompt(t *testing.T) {
	m := list.New(nil, rowDelegate{}, 240, 3)
	item := sessionItem{s: session.Session{
		Source: "test", SessionID: "session-1", ProjectLabel: "project",
		Title: "A distinct session title", FirstPrompt: "prompt belongs in details",
	}}
	var b bytes.Buffer
	(rowDelegate{}).Render(&b, m, 0, item)
	if !strings.Contains(b.String(), "A distinct session title") {
		t.Fatalf("rendered row does not contain session title: %q", b.String())
	}
	if strings.Contains(b.String(), "prompt belongs in details") {
		t.Fatalf("first prompt should not appear in the list row: %q", b.String())
	}
}

// groupedModel builds a picker over three sessions in two workspaces (one of
// them shared by two providers) without running a scan.
func groupedModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("RESUMER_TIPS_FILE", filepath.Join(t.TempDir(), "missing-tips.md"))
	m := NewModel(nil, session.Filters{})
	m.width, m.height = 120, 40
	m.resize()
	m.all = []session.Session{
		{Source: "kimi-code", SessionID: "k1", Cwd: "/ws/a", ProjectLabel: "a", Title: "a1", LastTS: "2026-09-29T10:00:00Z"},
		{Source: "opencode", SessionID: "o1", Cwd: "/ws/a", ProjectLabel: "a", Title: "a2", LastTS: "2026-09-29T09:00:00Z"},
		{Source: "kimi-code", SessionID: "k2", Cwd: "/ws/b", ProjectLabel: "b", Title: "b1", LastTS: "2026-09-29T08:00:00Z"},
	}
	m.applyItems()
	return m
}

func TestSessionListGroupsByWorkspace(t *testing.T) {
	m := groupedModel(t)
	if m.workspaces != 2 {
		t.Fatalf("workspaces = %d, want 2", m.workspaces)
	}
	items := m.list.Items()
	hdr, ok := items[0].(groupHeaderItem)
	if !ok {
		t.Fatalf("first item should be a workspace header, got %T", items[0])
	}
	if hdr.count != 2 || hdr.path != "/ws/a" {
		t.Errorf("merged header = %+v, want 2 sessions under /ws/a", hdr)
	}
	// selectable must point only at session rows, never headers.
	if len(m.selectable) != 3 {
		t.Fatalf("selectable = %v, want 3 session rows", m.selectable)
	}
	for _, idx := range m.selectable {
		if _, ok := items[idx].(sessionItem); !ok {
			t.Errorf("selectable[%d] = %T, want sessionItem", idx, items[idx])
		}
	}
	if view := m.View(); !strings.Contains(view, "3 sessions · 2 workspaces") {
		t.Errorf("status line should count sessions and workspaces:\n%s", view)
	}
}

func TestNavigationSkipsGroupHeaders(t *testing.T) {
	m := groupedModel(t)
	m.list.Select(m.selectable[0])
	for _, key := range []tea.KeyType{tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeyDown} {
		updated, _ := m.Update(tea.KeyMsg{Type: key})
		m = updated.(Model)
		if _, ok := m.list.SelectedItem().(sessionItem); !ok {
			t.Fatalf("down navigation landed on %T, want a session", m.list.SelectedItem())
		}
	}
	for i := 0; i < 5; i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = updated.(Model)
		if _, ok := m.list.SelectedItem().(sessionItem); !ok {
			t.Fatalf("up navigation landed on %T, want a session", m.list.SelectedItem())
		}
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = updated.(Model)
	if it, ok := m.list.SelectedItem().(sessionItem); !ok || it.s.SessionID != "k2" {
		t.Fatalf("End should select the last session, got %T", m.list.SelectedItem())
	}
}

func TestGroupHeaderIsNotFilterable(t *testing.T) {
	if got := (groupHeaderItem{path: "x", count: 2}).FilterValue(); got != "" {
		t.Errorf("group header FilterValue = %q, want empty so filters drop it", got)
	}
}

func TestSelectionPreservedAcrossSortToggle(t *testing.T) {
	m := groupedModel(t)
	m.list.Select(m.selectable[1]) // o1
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)
	it, ok := m.list.SelectedItem().(sessionItem)
	if !ok || it.s.SessionID != "o1" {
		t.Fatalf("sort toggle lost selection: got %T %+v", m.list.SelectedItem(), m.list.SelectedItem())
	}
}

// Toggling sort while a filter is active rebuilds the list; the selection must
// survive even though SetItems defers re-filtering to an async message.
func TestSelectionPreservedWhenSortingUnderFilter(t *testing.T) {
	m := groupedModel(t)
	m.list.SetFilterText("a") // synchronous fuzzy match → FilterApplied
	if m.list.FilterState() != list.FilterApplied {
		t.Fatalf("filter state = %v, want FilterApplied", m.list.FilterState())
	}
	m.restoreSelection("k1", "kimi-code")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("applyItems must return the list's re-filter command")
	}
	if msg := cmd(); msg != nil {
		updated, _ = m.Update(msg)
		m = updated.(Model)
	}
	it, ok := m.list.SelectedItem().(sessionItem)
	if !ok || it.s.SessionID != "k1" {
		t.Fatalf("selection lost across sort under filter: got %T %+v", m.list.SelectedItem(), m.list.SelectedItem())
	}
}

// The workspace holding the newest session must stay first, even after
// toggling the session sort to ascending.
func TestWorkspaceOrderFollowsNewestSession(t *testing.T) {
	m := groupedModel(t)
	if h, ok := m.list.Items()[0].(groupHeaderItem); !ok || h.path != "/ws/a" {
		t.Fatalf("default first header = %#v, want workspace /ws/a (newest)", m.list.Items()[0])
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)
	if h, ok := m.list.Items()[0].(groupHeaderItem); !ok || h.path != "/ws/a" {
		t.Fatalf("ascending sort must keep the newest workspace first, got %#v", m.list.Items()[0])
	}
}

func TestTipsPaneUsesSelectedProviderAndAdaptsToWidth(t *testing.T) {
	t.Setenv("RESUMER_TIPS_FILE", filepath.Join(t.TempDir(), "missing-tips.md"))
	m := NewModel([]provider.Provider{&refreshProvider{}}, session.Filters{})
	m.width, m.height = 120, 30
	m.resize()
	m.list.SetItems([]list.Item{sessionItem{s: session.Session{Source: "opencode", SessionID: "id"}}})
	m.preview.SetContent("session detail")
	wideView := m.View()
	if !strings.Contains(wideView, "opencode session delete") {
		t.Fatalf("wide view should show provider-specific tips:\n%s", wideView)
	}

	m.width = 90
	m.resize()
	if m.tipsWidth != 0 {
		t.Fatalf("narrow layout tips width = %d, want hidden tips pane", m.tipsWidth)
	}
}

func TestCustomTipsOverrideProviderDefaultsInPanel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tips.md")
	if err := os.WriteFile(path, []byte("My custom tips\nUse /resume to switch sessions."), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RESUMER_TIPS_FILE", path)

	m := NewModel([]provider.Provider{&refreshProvider{}}, session.Filters{})
	m.width, m.height = 120, 30
	m.resize()
	m.list.SetItems([]list.Item{sessionItem{s: session.Session{Source: "codebuddy", SessionID: "id"}}})
	m.preview.SetContent("session detail")
	view := m.View()
	if !strings.Contains(view, "My custom tips") || !strings.Contains(view, "Use /resume to switch sessions.") {
		t.Fatalf("custom tips were not rendered in the panel:\n%s", view)
	}
	if strings.Contains(view, "Rename: /rename") {
		t.Fatalf("custom tips should replace the built-in CodeBuddy tips:\n%s", view)
	}
}

func TestTipsForSourceExplainsProviderSpecificDeletion(t *testing.T) {
	if got := tipsForSource("opencode"); !strings.Contains(got, "opencode session delete") {
		t.Fatalf("OpenCode tips are missing delete command: %q", got)
	}
	if got := tipsForSource("kimi-code"); !strings.Contains(got, "Ctrl+X") {
		t.Fatalf("Kimi tips are missing picker shortcut: %q", got)
	}
	if got := tipsForSource("unknown-agent"); !strings.Contains(got, "differ by Agent/version") {
		t.Fatalf("generic tips should warn that deletion differs: %q", got)
	}
	codeBuddyTips := tipsForSource("codebuddy")
	for _, want := range []string{"/resume", "/rename", "DELETE /api/v1/sessions/:id", "--dry-run"} {
		if !strings.Contains(codeBuddyTips, want) {
			t.Errorf("CodeBuddy tips are missing %q: %q", want, codeBuddyTips)
		}
	}
}

func TestFormatLastActivity(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		ts, want string
	}{
		{"2026-09-25T06:30:00Z", "4d5h"},
		{"2026-09-29T07:55:00Z", "4h5m"},
		{"2026-09-29T11:35:00Z", "25m"},
		{"2026-09-29T12:01:00Z", "now"},
		{"invalid", "?"},
	}
	for _, tc := range cases {
		if got := formatLastActivity(tc.ts, now); got != tc.want {
			t.Errorf("formatLastActivity(%q) = %q, want %q", tc.ts, got, tc.want)
		}
	}
}

// The column header must line up with the fixed columns the rows use,
// otherwise the picker shows a header that points at nothing.
func TestColumnHeaderAlignsWithRowColumns(t *testing.T) {
	m := list.New(nil, rowDelegate{}, 240, 3)
	item := sessionItem{s: session.Session{
		Source: "kimi-code", SessionID: "session-1", ProjectLabel: "project",
		Title: "A distinct session title", Cwd: t.TempDir(),
	}}
	var row bytes.Buffer
	(rowDelegate{}).Render(&row, m, 0, item)

	head := ColumnHeader()
	for _, label := range []string{"age", "agent", "title"} {
		if !strings.Contains(head, label) {
			t.Errorf("column header is missing the %q label: %q", label, head)
		}
	}
	// Compare display columns, not byte offsets: the row's cursor glyph is
	// three bytes wide but occupies one column.
	plain := func(s string) string {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == 0x1b {
				for i < len(s) && s[i] != 'm' {
					i++
				}
				continue
			}
			b.WriteByte(s[i])
		}
		return b.String()
	}
	colOf := func(s, sub string) int {
		i := strings.Index(s, sub)
		if i < 0 {
			return -1
		}
		return textutil.DisplayWidth(s[:i])
	}
	h, r := plain(head), plain(row.String())
	for _, pair := range [][2]string{
		{"agent", "kimi-code"},          // header label vs the full agent name
		{"title", "A distinct session"}, // header label vs the title value
	} {
		hc, rc := colOf(h, pair[0]), colOf(r, pair[1])
		if hc < 0 || rc < 0 {
			t.Fatalf("could not locate %q in header/row: %q / %q", pair[0], h, r)
		}
		if hc != rc {
			t.Errorf("%q column starts at display column %d in the header but %d in the row:\nheader: %q\nrow:    %q",
				pair[0], hc, rc, h, r)
		}
	}
}
