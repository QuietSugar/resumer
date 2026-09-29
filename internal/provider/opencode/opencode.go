// Package opencode parses OpenCode (sst/opencode) session storage.
//
// Two generations exist on disk (both verified against the source):
//
//   - v1.1+ / v2: SQLite at $XDG_DATA_HOME/opencode/opencode.db
//     (stable channel; channel-suffixed opencode-<channel>.db otherwise).
//     session table: id/title/directory/parent_id and timestamps.
//     time_created/time_updated/time_archived (epoch milliseconds).
//     Prompts: session_message rows ({type:"user",text}) in v2; legacy
//     message+part table pairs in dbs migrated from 1.x.
//
//   - ≤ v1.0: JSON files under $XDG_DATA_HOME/opencode/storage/:
//     session/<projectID>/<ses_id>.json and message/<ses_id>/<msg_id>.json
//     (parts inline: [{type:"text",text,synthetic?}...]).
//
// Both are read when present and merged (SQLite wins on ID collision) so
// sessions stranded by a partial migration still show up. Subagent/child
// sessions (parent_id/parentID) and archived sessions are skipped, matching
// opencode's own session list. Resume: `opencode --session <id>`.
package opencode

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jin-ttao/resumer/internal/session"
	"github.com/jin-ttao/resumer/internal/sqliteread"
	"github.com/jin-ttao/resumer/internal/textutil"
)

const (
	envData = "RESUMER_OPENCODE_DATA"
	envBin  = "RESUMER_OPENCODE_BIN"
)

func dataRoot() string {
	if v := os.Getenv(envData); v != "" {
		return v
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "opencode")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode")
}

func storageRoot() string {
	return filepath.Join(dataRoot(), "storage", "session")
}

// dbPath prefers the stable opencode.db, falling back to the newest
// channel-suffixed variant (opencode-dev.db, opencode-beta.db, ...).
func dbPath() string {
	root := dataRoot()
	stable := filepath.Join(root, "opencode.db")
	if st, err := os.Stat(stable); err == nil && st.Mode().IsRegular() {
		return stable
	}
	matches, _ := filepath.Glob(filepath.Join(root, "opencode-*.db"))
	var newest string
	var newestMod time.Time
	for _, m := range matches {
		st, err := os.Stat(m)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if newest == "" || st.ModTime().After(newestMod) {
			newest, newestMod = m, st.ModTime()
		}
	}
	return newest
}

func opencodeBin() string {
	if bin := os.Getenv(envBin); bin != "" {
		return bin
	}
	return "opencode"
}

func binAvailable() bool {
	_, err := exec.LookPath(opencodeBin())
	return err == nil
}

// Provider implements provider.Provider for OpenCode.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string      { return "opencode" }
func (p *Provider) Badge() string     { return "oc" }
func (p *Provider) BadgeANSI() string { return "\x1b[34m" } // blue

func (p *Provider) IsAvailable() bool {
	if binAvailable() {
		if dbPath() != "" {
			return true
		}
		if st, err := os.Stat(storageRoot()); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

// msToTS converts epoch milliseconds to RFC3339 UTC (second precision, the
// granularity the shared renderers work at).
func msToTS(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.Unix(ms/1000, 0).UTC().Format(time.RFC3339)
}

// promptAgg accumulates first/last user prompts and assistant-turn counts for
// one session. Timestamps are epoch ms — comparable as ints.
type promptAgg struct {
	firstTS, lastTS int64
	first, last     string
	asst            int
	seenP           map[string]bool // "ts\x00text" — dedupes dual-projected rows
	seenA           map[string]bool // assistant turn identity (parent user ID, timestamp fallback)
}

func (a *promptAgg) addPrompt(ts int64, text string) {
	if text == "" {
		return
	}
	key := strconv.FormatInt(ts, 10) + "\x00" + text
	if a.seenP == nil {
		a.seenP = map[string]bool{}
	}
	if a.seenP[key] {
		return
	}
	a.seenP[key] = true
	if a.first == "" || (a.firstTS != 0 && ts < a.firstTS) || a.firstTS == 0 {
		if a.first == "" || ts < a.firstTS {
			a.firstTS, a.first = ts, text
		}
	}
	if a.last == "" || ts >= a.lastTS {
		a.lastTS, a.last = ts, text
	}
}

func (a *promptAgg) addAssistantTurn(parentID string, ts int64) {
	if a.seenA == nil {
		a.seenA = map[string]bool{}
	}
	key := "parent:" + parentID
	if parentID == "" {
		key = "time:" + strconv.FormatInt(ts, 10)
	}
	if a.seenA[key] {
		return
	}
	a.seenA[key] = true
	a.asst++
}

// indexSQLitePrompts walks the message-bearing tables of an opencode db and
// merges both projections at the row level. Baseline 1.18.x keeps two parallel
// projections of the same conversation: the durable pipeline writes
// session_message, while the v1-compat layer keeps writing message+part — and
// messages that predate an upgrade live only in the legacy pair (there is no
// backfill migration). Prompts are therefore unioned across the two families
// and deduped on (timestamp, text). Assistant messages are grouped by parent
// user-message ID so tool-call continuations count as one turn; timestamps are
// the fallback for older rows without parentID. Tolerates missing tables:
// pre-1.1 JSON-era installs have no db.
func indexSQLitePrompts(db *sqliteread.DB) map[string]*promptAgg {
	out := map[string]*promptAgg{}
	agg := func(id string) *promptAgg {
		a, ok := out[id]
		if !ok {
			a = &promptAgg{}
			out[id] = a
		}
		return a
	}

	// Durable projection (1.1+ … 1.18.x, v2): envelope column `type` plus a
	// JSON payload in `data`.
	if tbl, err := db.Table("session_message"); err == nil {
		_ = tbl.Scan(func(r sqliteread.Row) error {
			ses, _ := r.Str("session_id")
			if ses == "" {
				return nil
			}
			var payload struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ParentID string `json:"parentID"`
			}
			if raw, ok := r.Str("data"); ok && raw != "" {
				_ = json.Unmarshal([]byte(raw), &payload)
			} else {
				return nil
			}
			ts, _ := r.Int("time_created")
			switch payload.Type {
			case "user":
				agg(ses).addPrompt(ts, strings.TrimSpace(payload.Text))
			case "assistant":
				agg(ses).addAssistantTurn(payload.ParentID, ts)
			}
			return nil
		})
	}

	// v1-compat projection: message (role envelope) + part (typed blocks).
	// Legacy prompt timestamps come from message.time_created — part rows do
	// not carry usable prompt times of their own.
	type legacyMsg struct {
		ses  string
		role string
		ts   int64
	}
	msgs := map[string]legacyMsg{}
	if tbl, err := db.Table("message"); err == nil {
		_ = tbl.Scan(func(r sqliteread.Row) error {
			id, _ := r.Str("id")
			ses, _ := r.Str("session_id")
			if id == "" || ses == "" {
				return nil
			}
			var payload struct {
				Role     string `json:"role"`
				ParentID string `json:"parentID"`
			}
			if raw, ok := r.Str("data"); ok && raw != "" {
				_ = json.Unmarshal([]byte(raw), &payload)
			}
			if payload.Role != "user" && payload.Role != "assistant" {
				return nil
			}
			ts, _ := r.Int("time_created")
			msgs[id] = legacyMsg{ses: ses, role: payload.Role, ts: ts}
			if payload.Role == "assistant" {
				agg(ses).addAssistantTurn(payload.ParentID, ts)
			}
			return nil
		})
	}
	if len(msgs) > 0 {
		if tbl, err := db.Table("part"); err == nil {
			_ = tbl.Scan(func(r sqliteread.Row) error {
				mid, _ := r.Str("message_id")
				m, ok := msgs[mid]
				if !ok || m.role != "user" {
					return nil
				}
				var payload struct {
					Type      string `json:"type"`
					Text      string `json:"text"`
					Synthetic bool   `json:"synthetic"`
				}
				if raw, ok := r.Str("data"); ok && raw != "" {
					_ = json.Unmarshal([]byte(raw), &payload)
				}
				if payload.Type == "text" && !payload.Synthetic {
					agg(m.ses).addPrompt(m.ts, strings.TrimSpace(payload.Text))
				}
				return nil
			})
		}
	}
	return out
}

func projectLabel(dir string) string {
	if dir == "" {
		return "(unknown)"
	}
	if base := filepath.Base(strings.TrimRight(dir, "/")); base != "" && base != "/" && base != "." {
		return base
	}
	return "(unknown)"
}

func (p *Provider) readSQLite(path string) ([]session.Session, error) {
	db, err := sqliteread.Open(path)
	if err != nil {
		return nil, err
	}
	prompts := indexSQLitePrompts(db)
	tbl, err := db.Table("session")
	if err != nil {
		return nil, err
	}
	var out []session.Session
	err = tbl.Scan(func(r sqliteread.Row) error {
		id, _ := r.Str("id")
		if id == "" {
			return nil
		}
		if parent, _ := r.Str("parent_id"); parent != "" {
			return nil // child/subagent session
		}
		if v := r.Get("time_archived"); v != nil {
			return nil // archived
		}
		title, _ := r.Str("title")
		dir, _ := r.Str("directory")
		created, _ := r.Int("time_created")
		updated, _ := r.Int("time_updated")
		pa := prompts[id]

		out = append(out, session.Session{
			Source:       "opencode",
			SessionID:    id,
			Path:         fmt.Sprintf("%s#session/%s", path, id),
			ProjectLabel: projectLabel(dir),
			Cwd:          dir,
			FirstTS:      msToTS(created),
			LastTS:       msToTS(updated),
			Title:        strings.TrimSpace(title),
			FirstPrompt:  pa.first,
			LastPrompt:   pa.last,
			AsstCount:    pa.asst,
			ResumeArgv:   []string{"opencode", "--session", id},
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type jsonSessionFile struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectID"`
	Directory string `json:"directory"`
	ParentID  string `json:"parentID"`
	Title     string `json:"title"`
	Time      struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

type jsonPart struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Synthetic bool   `json:"synthetic"`
}

type jsonMessageFile struct {
	ID       string     `json:"id"`
	Role     string     `json:"role"`
	ParentID string     `json:"parentID"`
	Parts    []jsonPart `json:"parts"`
	Metadata struct {
		Time struct {
			Created int64 `json:"created"`
		} `json:"time"`
	} `json:"metadata"`
}

func (p *Provider) readJSON() ([]session.Session, error) {
	root := storageRoot()
	if _, err := os.Stat(root); err != nil {
		return nil, nil
	}
	var out []session.Session
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		var info jsonSessionFile
		if json.Unmarshal(data, &info) != nil || info.ID == "" {
			return nil
		}
		if info.ParentID != "" {
			return nil // child/subagent session
		}

		pa := &promptAgg{}
		msgDir := filepath.Join(dataRoot(), "storage", "message", info.ID)
		if entries, merr := os.ReadDir(msgDir); merr == nil {
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
					names = append(names, filepath.Join(msgDir, e.Name()))
				}
			}
			sort.Strings(names)
			for _, mp := range names {
				mdata, rerr := os.ReadFile(mp)
				if rerr != nil {
					continue
				}
				var msg jsonMessageFile
				if json.Unmarshal(mdata, &msg) != nil {
					continue
				}
				ts := msg.Metadata.Time.Created
				switch msg.Role {
				case "user":
					for _, part := range msg.Parts {
						if part.Type == "text" && !part.Synthetic {
							pa.addPrompt(ts, strings.TrimSpace(part.Text))
							break // first text block is the prompt
						}
					}
				case "assistant":
					pa.addAssistantTurn(msg.ParentID, ts)
				}
			}
		}
		out = append(out, session.Session{
			Source:       "opencode",
			SessionID:    info.ID,
			Path:         path,
			ProjectLabel: projectLabel(info.Directory),
			Cwd:          info.Directory,
			FirstTS:      msToTS(info.Time.Created),
			LastTS:       msToTS(info.Time.Updated),
			Title:        strings.TrimSpace(info.Title),
			FirstPrompt:  pa.first,
			LastPrompt:   pa.last,
			Prompts:      nil,
			AsstCount:    pa.asst,
			ResumeArgv:   []string{"opencode", "--session", info.ID},
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// listRaw returns every visible session, SQLite-first, merged with any
// legacy JSON-only sessions (deduped by ID; SQLite wins).
func (p *Provider) listRaw() ([]session.Session, error) {
	var out []session.Session
	seen := map[string]bool{}
	if path := dbPath(); path != "" {
		ss, err := p.readSQLite(path)
		if err != nil {
			return nil, fmt.Errorf("opencode db %s: %w", path, err)
		}
		for _, s := range ss {
			seen[s.SessionID] = true
		}
		out = append(out, ss...)
	}
	js, err := p.readJSON()
	if err != nil {
		return nil, err
	}
	for _, s := range js {
		if !seen[s.SessionID] {
			out = append(out, s)
		}
	}
	return out, nil
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

// cutoffForFilters: local midnight minus N days (provider default 3 when the
// CLI left Days unset). Nil when --all or --date is in play.
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

func (p *Provider) ListSessions(f session.Filters) ([]session.Session, error) {
	cutoff := cutoffForFilters(f)
	var day *time.Time
	if f.Date != "" {
		if d, err := time.ParseInLocation("2006-01-02", f.Date, time.UTC); err == nil {
			day = &d
		}
	}

	raw, err := p.listRaw()
	if err != nil {
		return nil, err
	}
	var out []session.Session
	for i := range raw {
		s := &raw[i]
		if f.Project != "" &&
			!strings.Contains(strings.ToLower(s.ProjectLabel), strings.ToLower(f.Project)) {
			continue
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
	raw, err := p.listRaw()
	if err != nil {
		return nil, err
	}
	for i := range raw {
		if raw[i].SessionID == id {
			return &raw[i], nil
		}
	}
	return nil, nil
}
