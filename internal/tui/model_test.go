package tui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/jin-ttao/resumer/internal/provider"
	"github.com/jin-ttao/resumer/internal/session"
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

func TestRowDelegateRendersTitleColumn(t *testing.T) {
	m := list.New(nil, rowDelegate{}, 240, 3)
	item := sessionItem{s: session.Session{
		Source: "test", SessionID: "session-1", ProjectLabel: "project",
		Title: "A distinct session title", FirstPrompt: "hello",
	}}
	var b bytes.Buffer
	(rowDelegate{}).Render(&b, m, 0, item)
	if !strings.Contains(b.String(), "A distinct session title") {
		t.Fatalf("rendered row does not contain session title: %q", b.String())
	}
}
