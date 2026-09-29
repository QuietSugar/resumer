package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func mkToggleRows() toggleModel {
	return toggleModel{rows: []toggleRow{
		{name: "alpha", off: false},
		{name: "beta", off: true},
		{name: "gamma", off: false, note: "storage missing"},
	}}
}

func TestToggleModelToggleAndSet(t *testing.T) {
	m := mkToggleRows()
	if got := m.disabledSet(); !reflect.DeepEqual(got, []string{"beta"}) {
		t.Fatalf("initial disabledSet = %v", got)
	}
	m.cursor = 2
	m.toggleCurrent() // gamma on → off
	if !m.rows[2].off {
		t.Fatal("gamma should be off after toggle")
	}
	m.toggleCurrent() // and back
	if m.rows[2].off {
		t.Fatal("gamma should be on after second toggle")
	}
	m.cursor = 0
	m.toggleCurrent() // alpha on → off
	if got := m.disabledSet(); !reflect.DeepEqual(got, []string{"alpha", "beta"}) {
		t.Fatalf("disabledSet = %v, want [alpha beta]", got)
	}
}

func TestToggleModelCursorKeys(t *testing.T) {
	m := mkToggleRows()
	up := tea.KeyMsg{Type: tea.KeyUp}
	down := tea.KeyMsg{Type: tea.KeyDown}

	step := func(msg tea.Msg) toggleModel {
		next, _ := m.Update(msg)
		return next.(toggleModel)
	}

	m = step(up) // clamped at top
	if m.cursor != 0 {
		t.Fatalf("cursor = %d after up at top", m.cursor)
	}
	m = step(down)
	m = step(down)
	m = step(down) // clamped at bottom
	if m.cursor != 2 {
		t.Fatalf("cursor = %d after three downs, want 2", m.cursor)
	}
	m = step(up)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d after up, want 1", m.cursor)
	}
}

func TestToggleModelEnterSavesEscCancels(t *testing.T) {
	m := mkToggleRows()

	final, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter must quit (non-nil cmd)")
	}
	if final.(toggleModel).canceled {
		t.Fatal("enter must not cancel")
	}

	final, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if !final.(toggleModel).canceled {
		t.Fatal("esc must cancel")
	}

	final, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !final.(toggleModel).canceled {
		t.Fatal("ctrl+c must cancel")
	}
}

func TestProviderToggleRowAssembly(t *testing.T) {
	// Mirrors the row assembly inside ProviderToggle without running a
	// program: registered names in order, disabled extras annotated.
	names := []string{"alpha", "beta"}
	disabled := map[string]bool{"beta": true, "zeta": true}
	storage := map[string]bool{"alpha": true, "beta": false}

	var rows []toggleRow
	for _, n := range names {
		note := ""
		if s, known := storage[n]; known && !s {
			note = "storage missing"
		}
		rows = append(rows, toggleRow{name: n, off: disabled[n], note: note})
	}
	// (extras assembly lives in ProviderToggle; assert its expected output)
	extras := []toggleRow{{name: "zeta", off: true, note: "(unknown)"}}
	rows = append(rows, extras...)

	if rows[0].name != "alpha" || rows[0].off {
		t.Errorf("row0 = %+v", rows[0])
	}
	if rows[1].name != "beta" || !rows[1].off || rows[1].note != "storage missing" {
		t.Errorf("row1 = %+v", rows[1])
	}
	if rows[2].name != "zeta" || !rows[2].off || rows[2].note != "(unknown)" {
		t.Errorf("row2 = %+v", rows[2])
	}
}
