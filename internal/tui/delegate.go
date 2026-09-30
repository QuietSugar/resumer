package tui

import (
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/QuietSugar/resumer/internal/cwd"
	"github.com/QuietSugar/resumer/internal/session"
	"github.com/QuietSugar/resumer/internal/textutil"
)

// Compact fixed-width columns keep the session list readable in narrow terminals.
const (
	colLast    = 7
	colBadge   = 7
	colProject = 16
	colTitle   = 24
)

var (
	badgeStyle = map[string]lipgloss.Style{
		"claude-code": lipgloss.NewStyle().Foreground(lipgloss.Color("2")), // green
		"codebuddy":   lipgloss.NewStyle().Foreground(lipgloss.Color("6")), // cyan
		"codex":       lipgloss.NewStyle().Foreground(lipgloss.Color("6")), // cyan
		"kimi-code":   lipgloss.NewStyle().Foreground(lipgloss.Color("5")), // magenta
		"opencode":    lipgloss.NewStyle().Foreground(lipgloss.Color("4")), // blue
	}
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	normalStyle   = lipgloss.NewStyle()
)

// sessionItem adapts session.Session to bubbles/list.
type sessionItem struct {
	s session.Session
}

// FilterValue exposes the same searchable surface fzf had: project label,
// first prompt, and title/subtitle.
func (i sessionItem) FilterValue() string {
	return i.s.ProjectLabel + " " + i.s.FirstPrompt + " " + i.s.Title + " " + i.s.Subtitle
}

// rowDelegate renders one session per line in the fixed-column layout.
type rowDelegate struct{}

func (d rowDelegate) Height() int                             { return 1 }
func (d rowDelegate) Spacing() int                            { return 0 }
func (d rowDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

func formatLastActivity(ts string, now time.Time) string {
	last, ok := textutil.ParseISO(ts)
	if !ok {
		return "?"
	}
	age := now.Sub(last)
	if age < time.Minute {
		return "now"
	}
	minutes := int(age / time.Minute)
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	hours := minutes / 60
	if hours < 24 {
		return fmt.Sprintf("%dh%dm", hours, minutes%60)
	}
	days := hours / 24
	return fmt.Sprintf("%dd%dh", days, hours%24)
}

func (d rowDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(sessionItem)
	if !ok {
		return
	}
	s := &it.s

	last := textutil.PadDisplay(formatLastActivity(s.LastTS, time.Now()), colLast)
	badgeText := "[" + s.Source + "]"
	if s.Source == "claude-code" {
		badgeText = "[cc]"
	}
	if s.Source == "codebuddy" {
		badgeText = "[cb]"
	}
	if s.Source == "kimi-code" {
		badgeText = "[kimi]"
	}
	if s.Source == "opencode" {
		badgeText = "[oc]"
	}
	badge := badgeStyle[s.Source].Render(textutil.PadDisplay(badgeText, colBadge))
	proj := textutil.PadDisplay(textutil.TrimDisplay(s.ProjectLabel, colProject), colProject)
	title := textutil.PadDisplay(textutil.TrimDisplay(s.Title, colTitle), colTitle)
	marker := textutil.PadDisplay(textutil.VolumeMarker(s.AsstCount+len(s.Prompts)), 2)

	selected := index == m.Index()
	cursor := "  "
	rowStyle := normalStyle
	if selected {
		cursor = cursorStyle.Render("▌ ")
		rowStyle = selectedStyle
	}

	row := fmt.Sprintf("%s%s %s %s %s %s",
		cursor, dimStyle.Render(last), badge, rowStyle.Render(proj), dimStyle.Render(title), marker)
	// A session whose working directory is gone cannot be resumed; say so in
	// the row itself, before the user presses enter.
	if cwd.Missing(s) {
		row += " " + warnStyle.Render(textutil.DirMissingMarker)
	}

	// Clip to the list's width so long rows never wrap and break the layout.
	fmt.Fprint(w, lipgloss.NewStyle().MaxWidth(m.Width()).Render(row))
}
