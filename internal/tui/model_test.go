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
	if got := tipsForSource("codex"); !strings.Contains(got, "differ by Agent/version") {
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
