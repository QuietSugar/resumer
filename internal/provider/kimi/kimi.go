// Package kimi parses Kimi Code CLI session files under $KIMI_CODE_HOME
// (default ~/.kimi-code).
//
// On-disk layout (per the official docs):
//
//	~/.kimi-code/
//	├── session_index.jsonl            # {sessionId, sessionDir, workDir} per line
//	└── sessions/
//	    └── <workDirKey>/             # wd_<slug>_<sha256[:12]>
//	        └── <sessionId>/
//	            ├── state.json        # title, lastPrompt, created/updated, forkedFrom
//	            └── agents/
//	                └── main/
//	                    └── wire.jsonl   # agent event stream (main agent only)
//
// Resume command: `kimi --session <sessionId>` (run from the session's
// recorded workDir, which resumer passes via Session.Cwd).
//
// Kimi Code 2.1.x stores durable v2 events in wire.jsonl: user inputs appear
// in turn.prompt/context.append_message and completed replies in turn.ended.
// Older role-based message events remain a best-effort fallback. Content may
// be a string or typed {type,text} blocks.
package kimi

import (
	"bufio"
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

	"github.com/QuietSugar/resumer/internal/session"
	"github.com/QuietSugar/resumer/internal/textutil"
)

const (
	envKimiHome  = "RESUMER_KIMI_HOME"
	envKimiBin   = "RESUMER_KIMI_BIN"
	maxLineBytes = 10 << 20
)

// Wire/state timestamp conventions (verified against kimi-code source,
// packages/agent-core-v2): SessionMeta.createdAt/updatedAt are epoch
// MILLISECONDS numbers; wire records carry `time` (epoch ms) while the
// metadata record uses `created_at`. ISO-8601 strings are accepted too for
// forward/backward compatibility.

// kimiHome mirrors Kimi Code's KIMI_CODE_HOME data root, with a resumer-level
// override for test harnesses.
func kimiHome() string {
	if v := os.Getenv(envKimiHome); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kimi-code")
}

func sessionRoot() string {
	return filepath.Join(kimiHome(), "sessions")
}

func indexFile() string {
	return filepath.Join(kimiHome(), "session_index.jsonl")
}

func kimiBin() string {
	if bin := os.Getenv(envKimiBin); bin != "" {
		return bin
	}
	return "kimi"
}

func kimiBinAvailable() bool {
	_, err := exec.LookPath(kimiBin())
	return err == nil
}

// Provider implements provider.Provider for Kimi Code CLI.
type Provider struct {
	indexCache map[string]string // sessionId → workDir, per resolved index path
	indexSeen  map[string]bool
}

func New() *Provider {
	return &Provider{
		indexCache: map[string]string{},
		indexSeen:  map[string]bool{},
	}
}

func (p *Provider) Name() string      { return "kimi-code" }
func (p *Provider) Badge() string     { return "kimi" }
func (p *Provider) BadgeANSI() string { return "\x1b[35m" } // magenta

func (p *Provider) IsAvailable() bool {
	st, err := os.Stat(sessionRoot())
	return err == nil && st.IsDir() && kimiBinAvailable()
}

// loadWorkDirs maps sessionId → workDir from session_index.jsonl. The index
// is optional enrichment (it only provides the cwd / project label), so a
// missing or corrupt file degrades silently instead of warning — unlike the
// codex index, kimi titles come from state.json and survive without it.
func (p *Provider) loadWorkDirs() map[string]string {
	path := indexFile()
	if p.indexSeen[path] {
		return p.indexCache
	}
	out := map[string]string{}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
		for sc.Scan() {
			var rec struct {
				SessionID string `json:"sessionId"`
				WorkDir   string `json:"workDir"`
			}
			if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
				continue
			}
			if rec.SessionID != "" && rec.WorkDir != "" {
				out[rec.SessionID] = rec.WorkDir
			}
		}
		f.Close()
	}
	p.indexSeen[path] = true
	p.indexCache = out
	return out
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type messageBody struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Origin  json.RawMessage `json:"origin"`
}

type wireRecord struct {
	// Time is the epoch-ms number written by every event (ExternalEvent.time).
	Time json.Number `json:"time"`
	// Timestamp accepts ISO strings for forward compatibility.
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	TurnID    json.RawMessage `json:"turnId"`
	PromptID  string          `json:"promptId"`
	Reason    string          `json:"reason"`
	Message   json.RawMessage `json:"message"`
	Content   json.RawMessage `json:"content"`
	Input     json.RawMessage `json:"input"`
	Origin    json.RawMessage `json:"origin"`
}

// recordTS resolves a record's timestamp: epoch-ms `time` first (the real
// kimi-code convention), then ISO `timestamp`.
func recordTS(r wireRecord) string {
	if r.Time != "" {
		if ts := normalizeTS(r.Time.String()); ts != "" {
			return ts
		}
	}
	return normalizeTS(r.Timestamp)
}

// extractText decodes content that is either a plain string or a list of
// typed blocks (text blocks joined with newlines).
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// isRealPrompt filters synthetic/system-injected user turns. Kimi marks such
// content with angle-bracket tags (same convention Claude Code uses).
func isRealPrompt(txt string) bool {
	s := strings.TrimSpace(txt)
	return s != "" && !strings.HasPrefix(s, "<")
}

// promptOrigin decodes both the current PromptOrigin object and the legacy
// string form stored in ContextMessage.origin. The boolean reports whether
// the origin was recognizable; unknown origins retain the legacy text filter.
func promptOrigin(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) == nil && value != "" {
		return value, true
	}
	var object struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(raw, &object) == nil && object.Kind != "" {
		return object.Kind, true
	}
	return "", false
}

func includePrompt(text string, origin json.RawMessage) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if kind, known := promptOrigin(origin); known {
		return kind == "user"
	}
	return isRealPrompt(text)
}

type promptCandidate struct {
	prompt session.Prompt
	source string // "turn.prompt" or "context.append_message"
	order  int
}

// mergePromptCandidates combines turn.prompt (the submitted user input) and
// context.append_message (the context transcript). The same prompt is written
// to both events, usually milliseconds apart; collapse only cross-source,
// same-text pairs close in time so intentionally repeated prompts survive.
func mergePromptCandidates(in []promptCandidate) []session.Prompt {
	paired := make([]bool, len(in))
	byText := map[string]map[string][]int{}
	for i, candidate := range in {
		text := candidate.prompt.Text
		if byText[text] == nil {
			byText[text] = map[string][]int{}
		}
		byText[text][candidate.source] = append(byText[text][candidate.source], i)
	}
	for _, sources := range byText {
		turns, messages := sources["turn.prompt"], sources["context.append_message"]
		for _, ti := range turns {
			tt, okT := textutil.ParseISO(in[ti].prompt.TS)
			if !okT {
				continue
			}
			best, bestDelta := -1, time.Duration(1<<63-1)
			for _, mi := range messages {
				if paired[mi] {
					continue
				}
				mt, okM := textutil.ParseISO(in[mi].prompt.TS)
				if !okM {
					continue
				}
				delta := tt.Sub(mt)
				if delta < 0 {
					delta = -delta
				}
				if delta <= 2*time.Second && delta < bestDelta {
					best, bestDelta = mi, delta
				}
			}
			if best >= 0 {
				paired[ti] = true // retain the transcript's timestamp/text
				paired[best] = true
			}
		}
	}

	candidates := make([]promptCandidate, 0, len(in))
	for i, candidate := range in {
		if paired[i] {
			if candidate.source == "context.append_message" {
				candidates = append(candidates, candidate)
			}
			continue
		}
		candidates = append(candidates, candidate)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		ti, okI := textutil.ParseISO(candidates[i].prompt.TS)
		tj, okJ := textutil.ParseISO(candidates[j].prompt.TS)
		if okI != okJ {
			return okI
		}
		if okI && !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return candidates[i].order < candidates[j].order
	})
	out := make([]session.Prompt, len(candidates))
	for i, candidate := range candidates {
		out[i] = candidate.prompt
	}
	return out
}

// normalizeTS passes ISO timestamps through untouched (the session struct and
// renderers expect RFC3339-ish strings) and converts bare epoch values —
// seconds or milliseconds — to UTC RFC3339 so sorting/rendering still work.
// Epochs shorter than 10 digits (pre-2001) are rejected so bogus 0 values
// can't poison lastTS.
func normalizeTS(ts string) string {
	if ts == "" {
		return ""
	}
	if _, ok := textutil.ParseISO(ts); ok {
		return ts
	}
	if digitsOnly(ts) {
		if len(ts) < 10 {
			return ""
		}
		n, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return ""
		}
		if len(ts) >= 13 { // milliseconds
			n = n / 1000
		}
		return time.Unix(n, 0).UTC().Format(time.RFC3339)
	}
	return ""
}

func digitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// laterTS / earlierTS compare by parsed instant but return the original
// string so formatting stays byte-identical to what's on disk.
func laterTS(a, b string) string {
	ta, okA := textutil.ParseISO(a)
	tb, okB := textutil.ParseISO(b)
	switch {
	case !okA && !okB:
		if a != "" {
			return a
		}
		return b
	case !okA:
		return b
	case !okB:
		return a
	case tb.After(ta):
		return b
	default:
		return a
	}
}

func earlierTS(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	ta, okA := textutil.ParseISO(a)
	tb, okB := textutil.ParseISO(b)
	if !okA {
		return b
	}
	if !okB {
		return a
	}
	if tb.Before(ta) {
		return b
	}
	return a
}

type stateMeta struct {
	Title      string      `json:"title"`
	LastPrompt string      `json:"lastPrompt"`
	CreatedAt  json.Number `json:"createdAt"` // epoch ms (real kimi-code form)
	UpdatedAt  json.Number `json:"updatedAt"`
	CreatedAlt string      `json:"created_at"` // ISO fallback form
	UpdatedAlt string      `json:"updated_at"`
	ForkedFrom string      `json:"forkedFrom"`
	ForkedAlt  string      `json:"forked_from"`
	Cwd        string      `json:"cwd"`
	WorkDir    string      `json:"workDir"`
}

// metaTS resolves state.json timestamps: epoch-ms number first, then ISO
// string fallback.
func metaTS(n json.Number, iso string) string {
	if n != "" {
		if ts := normalizeTS(n.String()); ts != "" {
			return ts
		}
	}
	return normalizeTS(iso)
}

// parseSessionDir reads one session directory (state.json + main wire.jsonl).
// Returns nil when state.json is missing/unreadable — a directory without it
// is not a session.
func (p *Provider) parseSessionDir(dir string) *session.Session {
	sessionID := filepath.Base(dir)

	stateData, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		return nil
	}
	var state stateMeta
	_ = json.Unmarshal(stateData, &state)

	createdAt := metaTS(state.CreatedAt, state.CreatedAlt)
	updatedAt := metaTS(state.UpdatedAt, state.UpdatedAlt)
	forkedFrom := state.ForkedFrom
	if forkedFrom == "" {
		forkedFrom = state.ForkedAlt
	}

	// The 2.1.x main-agent wire stream stores submitted prompts in
	// turn.prompt/context.append_message, completed replies in turn.ended,
	// and loop content in context.append_loop_event. Subagents are in separate
	// files and are not scanned.
	firstTS, lastTS := "", ""
	legacyAssistantCount := 0
	hasTurnEndedEvents, hasPromptCompletedEvents := false, false
	turnEnds, promptEnds := map[string]bool{}, map[string]bool{}
	var promptCandidates []promptCandidate
	if f, err := os.Open(filepath.Join(dir, "agents", "main", "wire.jsonl")); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
		lineOrder := 0
		for sc.Scan() {
			var r wireRecord
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				continue
			}
			lineOrder++
			if ts := recordTS(r); ts != "" {
				firstTS = earlierTS(firstTS, ts)
				lastTS = laterTS(lastTS, ts)
			}
			switch r.Type {
			case "turn.ended":
				hasTurnEndedEvents = true
				if r.Reason == "completed" {
					key := strings.TrimSpace(string(r.TurnID))
					if key == "" || key == "null" {
						key = recordTS(r)
					}
					turnEnds[key] = true
				}
			case "prompt.completed":
				hasPromptCompletedEvents = true
				if r.Reason == "completed" {
					key := r.PromptID
					if key == "" {
						key = recordTS(r)
					}
					promptEnds[key] = true
				}
			}

			if r.Type == "turn.prompt" {
				if txt := extractText(r.Input); includePrompt(txt, r.Origin) {
					promptCandidates = append(promptCandidates, promptCandidate{
						prompt: session.Prompt{TS: recordTS(r), Text: strings.TrimSpace(txt)},
						source: "turn.prompt",
						order:  lineOrder,
					})
				}
			}

			role, content := r.Role, r.Content
			origin := r.Origin
			if len(r.Message) > 0 {
				var msg messageBody
				if err := json.Unmarshal(r.Message, &msg); err == nil {
					if msg.Role != "" {
						role = msg.Role
					}
					if len(msg.Content) > 0 {
						content = msg.Content
					}
					if len(msg.Origin) > 0 {
						origin = msg.Origin
					}
				}
			}
			if r.Type == "user_message" && role == "" {
				role = "user"
			}
			switch role {
			case "user":
				if txt := extractText(content); includePrompt(txt, origin) {
					promptCandidates = append(promptCandidates, promptCandidate{
						prompt: session.Prompt{TS: recordTS(r), Text: strings.TrimSpace(txt)},
						source: "context.append_message",
						order:  lineOrder,
					})
				}
			case "assistant":
				legacyAssistantCount++
			}
		}
		f.Close()
	}
	prompts := mergePromptCandidates(promptCandidates)
	asstCount := legacyAssistantCount
	switch {
	case hasTurnEndedEvents:
		asstCount = len(turnEnds)
	case hasPromptCompletedEvents:
		asstCount = len(promptEnds)
	}
	firstTS = earlierTS(firstTS, createdAt)
	lastTS = laterTS(lastTS, updatedAt)

	cwd := state.WorkDir
	if cwd == "" {
		cwd = state.Cwd
	}
	if cwd == "" {
		cwd = p.loadWorkDirs()[sessionID]
	}

	projectLabel := "(unknown)"
	if cwd != "" {
		if base := filepath.Base(strings.TrimRight(cwd, "/")); base != "" && base != "/" && base != "." {
			projectLabel = base
		}
	}

	subtitle := ""
	if forkedFrom != "" {
		short := forkedFrom
		if len(short) > 8 {
			short = short[:8]
		}
		subtitle = fmt.Sprintf("forked from %s", short)
	}

	firstPrompt, lastPrompt := "", ""
	if len(prompts) > 0 {
		firstPrompt = prompts[0].Text
		lastPrompt = prompts[len(prompts)-1].Text
	}
	if lastPrompt == "" {
		lastPrompt = strings.TrimSpace(state.LastPrompt)
	}
	title := strings.TrimSpace(state.Title)
	if title == "" {
		title = lastPrompt // native Kimi session listings fall back to lastPrompt
	}

	return &session.Session{
		Source:       "kimi-code",
		SessionID:    sessionID,
		Path:         filepath.Join(dir, "state.json"),
		ProjectLabel: projectLabel,
		Cwd:          cwd,
		FirstTS:      firstTS,
		LastTS:       lastTS,
		Title:        title,
		Subtitle:     subtitle,
		FirstPrompt:  firstPrompt,
		LastPrompt:   lastPrompt,
		Prompts:      prompts,
		AsstCount:    asstCount,
		ResumeArgv:   []string{"kimi", "--session", sessionID},
	}
}

// sessionDirs returns the <workDirKey>/<sessionId> directories that contain a
// state.json. Descend only two levels; session internals (agents/, tasks/,
// cron/) are never walked.
func sessionDirs(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path == root {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return filepath.SkipDir
		}
		depth := len(strings.Split(rel, string(filepath.Separator)))
		if depth == 1 {
			return nil // workDirKey bucket — descend into it
		}
		if depth == 2 {
			if _, err := os.Stat(filepath.Join(path, "state.json")); err == nil {
				out = append(out, path)
			}
		}
		return filepath.SkipDir // never descend into session internals
	})
	sort.Strings(out)
	return out
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

// cutoffForFilters: local midnight minus N days. Nil when there is no time
// window at all — --all, --date, or the CLI default of "no limit" (Days == 0)
// — so every parsed session is kept.
func cutoffForFilters(f session.Filters) *time.Time {
	if f.AllTime || f.Date != "" || f.Days == 0 {
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
	root := sessionRoot()
	cutoff := cutoffForFilters(f)
	var day *time.Time
	if f.Date != "" {
		if d, err := time.ParseInLocation("2006-01-02", f.Date, time.UTC); err == nil {
			day = &d
		}
	}

	var out []session.Session
	for _, dir := range sessionDirs(root) {
		s := p.parseSessionDir(dir)
		if s == nil {
			continue
		}
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
	for _, dir := range sessionDirs(sessionRoot()) {
		if filepath.Base(dir) == id {
			return p.parseSessionDir(dir), nil
		}
	}
	return nil, nil
}
