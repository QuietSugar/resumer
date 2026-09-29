// Package render holds the provider-agnostic renderers — index table, full
// detail box (also the TUI preview), and JSON. The list output keeps raw ANSI
// escapes (not lipgloss) so `resumer list` stays byte-stable for scripts and
// the QA assertions; NO_COLOR is honored manually, as before.
package render

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/jin-ttao/resumer/internal/session"
	"github.com/jin-ttao/resumer/internal/textutil"
)

const ansiReset = "\x1b[0m"

// BadgeANSI maps source → badge color. Kept here so the renderer does not
// import provider packages.
var BadgeANSI = map[string]string{
	"claude-code": "\x1b[32m", // green
	"codex":       "\x1b[36m", // cyan
	"kimi-code":   "\x1b[35m", // magenta
}

const (
	FirstPromptWidth = 78
	AuxWidth         = 46
)

func noColor() bool {
	return os.Getenv("NO_COLOR") != ""
}

// Badge renders a fixed-width badge like "[cc]   ", "[codex]" or "[kimi] ", 7 visible cols.
func Badge(source, ansi string) string {
	text := "[" + source + "]"
	switch source {
	case "claude-code":
		text = "[cc]"
	case "kimi-code":
		text = "[kimi]"
	}
	padded := textutil.PadDisplay(text, 7)
	if noColor() {
		return padded
	}
	return ansi + padded + ansiReset
}

// FmtLastShort renders the "MM-DD HH:MM:SS" form used in the index table.
func FmtLastShort(ts string) string {
	return textutil.FmtTS(ts, false)
}

// Index renders the compact one-line-per-session table.
func Index(sessions []session.Session) string {
	if len(sessions) == 0 {
		return "(no sessions)"
	}
	var out []string
	out = append(out, fmt.Sprintf(
		"%-17s %-7s %-25s %-3s %9s  %s",
		"last_activity", "src", "project", "mk", "tokens", "first prompt"))
	out = append(out, strings.Repeat("─", 140))
	for i := range sessions {
		s := &sessions[i]
		last := textutil.PadDisplay(FmtLastShort(s.LastTS), 17)
		badge := Badge(s.Source, BadgeANSI[s.Source])
		proj := textutil.PadDisplay(textutil.TrimDisplay(s.ProjectLabel, 25), 25)
		msgs := s.AsstCount + len(s.Prompts)
		markers := textutil.VolumeMarker(msgs) + "  "
		var tokTotal int64
		if s.Tokens != nil {
			tokTotal = s.Tokens.Input
		}
		tokCol := fmt.Sprintf("%9s", textutil.FmtTokens(tokTotal))
		first := textutil.TrimDisplay(s.FirstPrompt, FirstPromptWidth)
		firstPadded := textutil.PadDisplay(first, FirstPromptWidth)
		aux := ""
		if s.Title != "" {
			aux = textutil.TrimDisplay(s.Title, AuxWidth)
		} else if s.Subtitle != "" {
			aux = textutil.TrimDisplay(s.Subtitle, AuxWidth)
		}
		label := firstPadded
		if aux != "" {
			label = firstPadded + "  " + aux
		}
		out = append(out, fmt.Sprintf("%s %s %s %s %s  %s", last, badge, proj, markers, tokCol, label))
	}
	return strings.Join(out, "\n")
}

// FullBox renders the single-session detail box (used as the TUI preview).
func FullBox(s *session.Session) string {
	const width = 72
	bar := strings.Repeat("─", width)
	lines := []string{"┌" + bar}
	add := func(format string, a ...any) {
		lines = append(lines, fmt.Sprintf(format, a...))
	}
	cwd := s.Cwd
	if cwd == "" {
		cwd = "(none)"
	}
	add("│ source:         [%s]", s.Source)
	add("│ 📁 project:     %s", s.ProjectLabel)
	add("│ session id:     %s", s.SessionID)
	add("│ started:        %s", textutil.FmtTS(s.FirstTS, true))
	add("│ last activity:  %s", textutil.FmtTS(s.LastTS, true))
	add("│ duration:       %s", textutil.FmtDuration(s.FirstTS, s.LastTS))
	add("│ cwd:            %s", cwd)
	add("│ prompts:        %d user / %d assistant", len(s.Prompts), s.AsstCount)
	if s.Title != "" {
		add("│ title:          %s", s.Title)
	}
	if s.Subtitle != "" {
		add("│ context:        %s", s.Subtitle)
	}
	if s.Tokens != nil && s.Tokens.Turns > 0 {
		totalIn := s.Tokens.Input
		cacheHit := int64(0)
		if totalIn > 0 {
			cacheHit = int64(math.Round(float64(s.Tokens.CacheRead) / float64(totalIn) * 100))
		}
		avgIn := int64(0)
		if s.Tokens.Turns > 0 {
			avgIn = totalIn / s.Tokens.Turns
		}
		add("│ ── tokens ──")
		add("│ input:          %10s    (cache hit %d%%)", groupedInt(totalIn), cacheHit)
		add("│ output:         %10s", groupedInt(s.Tokens.Output))
		add("│ cache:          %s read / %s created",
			textutil.FmtTokens(s.Tokens.CacheRead), textutil.FmtTokens(s.Tokens.CacheCreate))
		add("│ avg input/turn: %10s", groupedInt(avgIn))
	}
	lines = append(lines, "├"+bar)
	lines = append(lines, "│ opening prompts")
	openEnd := len(s.Prompts)
	if openEnd > 3 {
		openEnd = 3
	}
	for i := 0; i < openEnd; i++ {
		add("│  [%d] %s", i+1, textutil.Trim(s.Prompts[i].Text, 350))
	}
	lastStart := len(s.Prompts) - 2
	if lastStart < openEnd {
		lastStart = openEnd
	}
	if lastStart < len(s.Prompts) {
		lines = append(lines, "│")
		lines = append(lines, "│ last prompts")
		for i := lastStart; i < len(s.Prompts); i++ {
			add("│  [%d] %s", i-lastStart+1, textutil.Trim(s.Prompts[i].Text, 350))
		}
	}
	lines = append(lines, "└"+bar)
	return strings.Join(lines, "\n")
}

// groupedInt formats with thousands separators (Python's {:,}).
func groupedInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}
