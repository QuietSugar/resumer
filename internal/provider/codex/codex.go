// Package codex parses Codex CLI rollout files under ~/.codex/sessions.
//
// JSONL structure:
//   - first line: type=session_meta, payload={id, cwd, timestamp, ...}
//   - later lines: type=event_msg with payload.type ∈ {task_started, user_message, ...}
//     (real user prompts appear only as event_msg/user_message)
//   - every line carries a top-level timestamp
//
// Titles (thread_name) come from ~/.codex/session_index.jsonl.
package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/QuietSugar/resumer/internal/session"
	"github.com/QuietSugar/resumer/internal/textutil"
)

const (
	envSessionRoot = "RESUMER_CODEX_SESSION_ROOT"
	envIndexFile   = "RESUMER_CODEX_INDEX_FILE"
	envCodexBin    = "RESUMER_CODEX_BIN"
	maxLineBytes   = 10 << 20
)

var rolloutDateRE = regexp.MustCompile(`^rollout-(\d{4}-\d{2}-\d{2})T`)

func sessionRoot() string {
	if v := os.Getenv(envSessionRoot); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "sessions")
}

func indexFile() string {
	if v := os.Getenv(envIndexFile); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "session_index.jsonl")
}

func codexBinAvailable() bool {
	bin := os.Getenv(envCodexBin)
	if bin == "" {
		bin = "codex"
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// Provider implements provider.Provider for Codex CLI.
type Provider struct {
	indexCache  map[string]map[string]string
	indexWarned map[string]bool
}

func New() *Provider {
	return &Provider{
		indexCache:  map[string]map[string]string{},
		indexWarned: map[string]bool{},
	}
}

func (p *Provider) Name() string      { return "codex" }
func (p *Provider) Badge() string     { return "codex" }
func (p *Provider) BadgeANSI() string { return "\x1b[36m" } // cyan

func (p *Provider) IsAvailable() bool {
	st, err := os.Stat(sessionRoot())
	return err == nil && st.IsDir() && codexBinAvailable()
}

// loadIndex maps session_id → thread_name. Graceful on missing/corrupt;
// warns once per path; cached per resolved path.
func (p *Provider) loadIndex() map[string]string {
	path := indexFile()
	if cached, ok := p.indexCache[path]; ok {
		return cached
	}
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		if !p.indexWarned[path] {
			fmt.Fprintf(os.Stderr,
				"resumer: codex session_index missing or unreadable (%s); titles will be unavailable\n", path)
			p.indexWarned[path] = true
		}
		p.indexCache[path] = out
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	for sc.Scan() {
		var r struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		if r.ID != "" && strings.TrimSpace(r.ThreadName) != "" {
			out[r.ID] = strings.TrimSpace(r.ThreadName)
		}
	}
	p.indexCache[path] = out
	return out
}

type codexRecord struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// parseJSONL parses one rollout file. Returns nil on missing session_meta.
func (p *Provider) parseJSONL(path string) *session.Session {
	var sessionID, cwd, firstTS, lastTS string
	var prompts []session.Prompt
	eventCount := 0

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	lineno := 0
	for sc.Scan() {
		lineno++
		var r codexRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		if r.Timestamp != "" {
			if firstTS == "" {
				firstTS = r.Timestamp
			}
			lastTS = r.Timestamp
		}
		if lineno == 1 {
			if r.Type != "session_meta" {
				fmt.Fprintf(os.Stderr,
					"resumer: codex rollout missing session_meta on first line, skipping: %s\n", path)
				return nil
			}
			var meta struct {
				ID        string `json:"id"`
				Cwd       string `json:"cwd"`
				Timestamp string `json:"timestamp"`
			}
			_ = json.Unmarshal(r.Payload, &meta)
			sessionID = meta.ID
			cwd = meta.Cwd
			// payload.timestamp is the authoritative start; prefer over top-level
			if meta.Timestamp != "" {
				firstTS = meta.Timestamp
			}
		} else if r.Type == "event_msg" {
			eventCount++
			var ev struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal(r.Payload, &ev)
			if ev.Type == "user_message" && strings.TrimSpace(ev.Message) != "" {
				prompts = append(prompts, session.Prompt{
					TS:   r.Timestamp,
					Text: strings.TrimSpace(ev.Message),
				})
			}
		}
	}

	if sessionID == "" {
		return nil
	}

	title := p.loadIndex()[sessionID]
	projectLabel := "(unknown)"
	if cwd != "" {
		if base := filepath.Base(strings.TrimRight(cwd, "/")); base != "" && base != "/" && base != "." {
			projectLabel = base
		}
	}

	firstPrompt, lastPrompt := "", ""
	if len(prompts) > 0 {
		firstPrompt = prompts[0].Text
		lastPrompt = prompts[len(prompts)-1].Text
	}

	return &session.Session{
		Source:       "codex",
		SessionID:    sessionID,
		Path:         path,
		ProjectLabel: projectLabel,
		Cwd:          cwd,
		FirstTS:      firstTS,
		LastTS:       lastTS,
		Title:        title,
		FirstPrompt:  firstPrompt,
		LastPrompt:   lastPrompt,
		Prompts:      prompts,
		AsstCount:    eventCount, // codex doesn't split assistant count cleanly; use event count
		ResumeArgv:   []string{"codex", "resume", sessionID},
	}
}

func rolloutDateFromName(fn string) (time.Time, bool) {
	m := rolloutDateRE.FindStringSubmatch(fn)
	if m == nil {
		return time.Time{}, false
	}
	d, err := time.ParseInLocation("2006-01-02", m[1], time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return d, true
}

func findRolloutFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
	return out
}

func cutoffForFilters(f session.Filters) *time.Time {
	if f.AllTime || f.Date != "" {
		return nil
	}
	days := f.Days
	if days < 0 {
		days = 3
	}
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	cutoff := midnight.AddDate(0, 0, -days)
	return &cutoff
}

func touchesDate(s *session.Session, day time.Time) bool {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	end := start.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	fs_, okF := textutil.ParseISO(s.FirstTS)
	ls, okL := textutil.ParseISO(s.LastTS)
	if !okF && !okL {
		return false
	}
	if !okF {
		fs_ = ls
	}
	if !okL {
		ls = fs_
	}
	return !(ls.Before(start) || fs_.After(end))
}

func (p *Provider) ListSessions(f session.Filters) ([]session.Session, error) {
	root := sessionRoot()
	cutoff := cutoffForFilters(f)
	var day *time.Time
	if f.Date != "" {
		if d, err := time.ParseInLocation("2006-01-02", f.Date, time.UTC); err == nil {
			day = &d
		}
	}

	var out []session.Session
	for _, path := range findRolloutFiles(root) {
		// Pre-filter by filename date (rollout-YYYY-MM-DDT...) to avoid
		// parsing historical files outside the requested window.
		if fdate, ok := rolloutDateFromName(filepath.Base(path)); ok {
			if day != nil {
				if !fdate.Equal(time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)) {
					continue
				}
			} else if cutoff != nil && fdate.Before(cutoff.Add(-24*time.Hour)) {
				// One-day slack for timezone drift across rollover boundary.
				continue
			}
		}
		s := p.parseJSONL(path)
		if s == nil {
			continue
		}
		if f.Project != "" {
			cwdTail := filepath.Base(s.Cwd)
			if s.Cwd == "" {
				cwdTail = ""
			}
			if !strings.Contains(strings.ToLower(cwdTail), strings.ToLower(f.Project)) {
				continue
			}
		}
		if day != nil {
			if !touchesDate(s, *day) {
				continue
			}
		} else if cutoff != nil {
			ls, ok := textutil.ParseISO(s.LastTS)
			if !ok || ls.Before(*cutoff) {
				continue
			}
		}
		out = append(out, *s)
	}
	return out, nil
}

func (p *Provider) LoadDetail(id string) (*session.Session, error) {
	suffix := "-" + id + ".jsonl"
	for _, path := range findRolloutFiles(sessionRoot()) {
		if strings.HasSuffix(filepath.Base(path), suffix) {
			s := p.parseJSONL(path)
			if s != nil && s.SessionID == id {
				return s, nil
			}
		}
	}
	return nil, nil
}
