package tui

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jin-ttao/resumer/internal/session"
	"github.com/jin-ttao/resumer/internal/textutil"
)

// Column widths mirror the old fzf row so muscle memory carries over.
const (
	colLast    = 15
	colBadge   = 7
	colProject = 22
	colPrompt  = 78
	colAux     = 40
)

var (
	badgeStyle = map[string]lipgloss.Style{
		"claude-code": lipgloss.NewStyle().Foreground(lipgloss.Color("2")), // green
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

func (d rowDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(sessionItem)
	if !ok {
		return
	}
	s := &it.s

	last := textutil.PadDisplay(textutil.FmtTS(s.LastTS, false), colLast)
	badgeText := "[" + s.Source + "]"
	if s.Source == "claude-code" {
		badgeText = "[cc]"
	}
	if s.Source == "kimi-code" {
		badgeText = "[kimi]"
	}
	if s.Source == "opencode" {
		badgeText = "[oc]"
	}
	badge := badgeStyle[s.Source].Render(textutil.PadDisplay(badgeText, colBadge))
	proj := textutil.PadDisplay(textutil.TrimDisplay(s.ProjectLabel, colProject), colProject)
	marker := textutil.PadDisplay(textutil.VolumeMarker(s.AsstCount+len(s.Prompts)), 2)
	var tokTotal int64
	if s.Tokens != nil {
		tokTotal = s.Tokens.Input
	}
	tok := fmt.Sprintf("%9s", textutil.FmtTokens(tokTotal))
	first := textutil.PadDisplay(textutil.TrimDisplay(s.FirstPrompt, colPrompt), colPrompt)
	aux := ""
	if s.Title != "" {
		aux = textutil.TrimDisplay(s.Title, colAux)
	} else if s.Subtitle != "" {
		aux = textutil.TrimDisplay(s.Subtitle, colAux)
	}

	selected := index == m.Index()
	cursor := "  "
	rowStyle := normalStyle
	if selected {
		cursor = cursorStyle.Render("▌ ")
		rowStyle = selectedStyle
	}

	label := rowStyle.Render(first)
	if aux != "" {
		label += "  " + dimStyle.Render(aux)
	}
	row := fmt.Sprintf("%s%s %s %s %s %s  %s",
		cursor, dimStyle.Render(last), badge, rowStyle.Render(proj), marker, dimStyle.Render(tok), label)

	// Clip to the list's width so long rows never wrap and break the layout.
	fmt.Fprint(w, lipgloss.NewStyle().MaxWidth(m.Width()).Render(row))
}
