package tui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/QuietSugar/resumer/internal/session"
	"github.com/QuietSugar/resumer/internal/textutil"
)

// Compact fixed-width columns keep the session list readable in narrow terminals.
const (
	colLast  = 7
	colAgent = 9 // widest full provider name: "codebuddy" / "kimi-code"
	colTitle = 40
)

var (
	badgeStyle = map[string]lipgloss.Style{
		"codebuddy": lipgloss.NewStyle().Foreground(lipgloss.Color("6")), // cyan
		"kimi-code": lipgloss.NewStyle().Foreground(lipgloss.Color("5")), // magenta
		"opencode":  lipgloss.NewStyle().Foreground(lipgloss.Color("4")), // blue
	}
	dimStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	colHeaderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Bold(true)
	groupHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Bold(true)
	selectedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	cursorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	normalStyle      = lipgloss.NewStyle()
	// volumeStyle is a heat ramp for the conversation-weight bar: the bigger
	// the session, the hotter the color.
	volumeStyle = map[string]lipgloss.Style{
		" ": normalStyle,
		"▁": dimStyle,
		"▄": lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		"█": lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
	}
)

// ColumnHeader renders the list's column header, aligned to the fixed columns
// rowDelegate.Render uses (2-col cursor prefix, then one space between
// columns). Keeping it here next to the column widths is what stops the header
// and the rows from drifting apart.
func ColumnHeader() string {
	head := strings.Join([]string{
		textutil.PadDisplay("age", colLast),
		textutil.PadDisplay("agent", colAgent),
		textutil.PadDisplay("title", colTitle),
	}, " ")
	return colHeaderStyle.Render("  " + head)
}

// sessionItem adapts session.Session to bubbles/list.
type sessionItem struct {
	s session.Session
}

// FilterValue exposes the same searchable surface fzf had: project label,
// first prompt, and title/subtitle.
func (i sessionItem) FilterValue() string {
	return i.s.ProjectLabel + " " + i.s.FirstPrompt + " " + i.s.Title + " " + i.s.Subtitle
}

// groupHeaderItem is a non-selectable workspace header row. Its FilterValue is
// empty so a non-empty filter drops it, leaving a flat list of matches.
type groupHeaderItem struct {
	count   int
	path    string
	deleted bool
}

func (i groupHeaderItem) FilterValue() string { return "" }

func (i groupHeaderItem) String() string {
	noun := "sessions"
	if i.count == 1 {
		noun = "session"
	}
	line := fmt.Sprintf("%d %s  ·  %s", i.count, noun, i.path)
	if i.deleted {
		line += "  " + textutil.DirDeletedLabel
	}
	return line
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
	switch it := item.(type) {
	case groupHeaderItem:
		line := groupHeaderStyle.Render(it.String())
		fmt.Fprint(w, lipgloss.NewStyle().MaxWidth(m.Width()).Render(line))
		return
	case sessionItem:
		d.renderSession(w, m, index, &it.s)
	}
}

func (d rowDelegate) renderSession(w io.Writer, m list.Model, index int, s *session.Session) {
	last := textutil.PadDisplay(formatLastActivity(s.LastTS, time.Now()), colLast)
	agent := badgeStyle[s.Source].Render(textutil.PadDisplay(s.Source, colAgent))
	title := textutil.PadDisplay(textutil.TrimDisplay(s.Title, colTitle), colTitle)
	glyph := textutil.VolumeMarker(s.AsstCount + len(s.Prompts))
	marker := volumeStyle[glyph].Render(textutil.PadDisplay(glyph, 2))

	selected := index == m.Index()
	cursor := "  "
	rowStyle := normalStyle
	if selected {
		cursor = cursorStyle.Render("▌ ")
		rowStyle = selectedStyle
	}

	row := fmt.Sprintf("%s%s %s %s %s",
		cursor, dimStyle.Render(last), agent, rowStyle.Render(title), marker)

	// Clip to the list's width so long rows never wrap and break the layout.
	fmt.Fprint(w, lipgloss.NewStyle().MaxWidth(m.Width()).Render(row))
}
