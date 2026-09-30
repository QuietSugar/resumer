// Package textutil holds shared formatters ported from the Python utils.py.
// Display-width handling is CJK-aware via go-runewidth (Korean text is a
// first-class use case).
package textutil

import (
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"
)

// ParseISO parses an RFC3339-ish timestamp. Returns ok=false on empty or
// unparseable input. Naive timestamps (no offset) are treated as UTC.
func ParseISO(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t, true
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, ts, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// FmtTS renders "YYYY-MM-DD HH:MM:SS" (or without year when includeYear is
// false). Pure string surgery, mirroring the Python implementation.
func FmtTS(ts string, includeYear bool) string {
	if ts == "" {
		return "?"
	}
	s := strings.Replace(ts, "T", " ", 1)
	if len(s) > 19 {
		s = s[:19]
	}
	if !includeYear && len(s) >= 10 && s[4] == '-' {
		return s[5:]
	}
	return s
}

// FmtDuration renders the gap between two timestamps as "<1min", "Nmin", "XhYm".
func FmtDuration(a, b string) string {
	da, okA := ParseISO(a)
	db, okB := ParseISO(b)
	if !okA || !okB {
		return "?"
	}
	mins := int(db.Sub(da).Minutes())
	if mins < 1 {
		return "<1min"
	}
	if mins < 60 {
		return fmt.Sprintf("%dmin", mins)
	}
	return fmt.Sprintf("%dh%dm", mins/60, mins%60)
}

func oneLine(txt string) string {
	r := strings.NewReplacer("\n", " ", "\r", " ", "\t", " ")
	return r.Replace(txt)
}

// Trim collapses whitespace control chars and cuts to limit code points
// (appending "…" when truncated). Counts runes, not display width.
func Trim(txt string, limit int) string {
	one := oneLine(txt)
	runes := []rune(one)
	if len(runes) <= limit {
		return one
	}
	cut := limit - 1
	if cut < 0 {
		cut = 0
	}
	return string(runes[:cut]) + "…"
}

// DisplayWidth is the terminal cell width of s (CJK chars count as 2).
func DisplayWidth(s string) int {
	return runewidth.StringWidth(s)
}

// TrimDisplay truncates to max display width, appending "…" when cut.
// Hand-rolled loop (not runewidth.Truncate) to mirror the Python boundary
// behavior exactly: accumulated width never exceeds maxW-1 before the "…".
func TrimDisplay(txt string, maxW int) string {
	one := oneLine(txt)
	if DisplayWidth(one) <= maxW {
		return one
	}
	var b strings.Builder
	w := 0
	for _, ch := range one {
		cw := runewidth.RuneWidth(ch)
		if w+cw > maxW-1 {
			break
		}
		b.WriteRune(ch)
		w += cw
	}
	return b.String() + "…"
}

// PadDisplay right-pads s with spaces to the target display width.
func PadDisplay(s string, targetW int) string {
	w := DisplayWidth(s)
	if w >= targetW {
		return s
	}
	return s + strings.Repeat(" ", targetW-w)
}

// DirMissingMarker flags a session whose recorded working directory no longer
// exists, so it cannot be resumed until that directory is recreated. Shared by
// the list renderer, the detail box, and the picker row so all three agree.
const DirMissingMarker = "⚠ dir missing"

// VolumeMarker is the fixed 1-col conversation weight marker:
// <20 blank / 20-49 · / 50-149 ● / 150+ ◉
func VolumeMarker(totalMsgs int) string {
	switch {
	case totalMsgs < 20:
		return " "
	case totalMsgs < 50:
		return "·"
	case totalMsgs < 150:
		return "●"
	default:
		return "◉"
	}
}
