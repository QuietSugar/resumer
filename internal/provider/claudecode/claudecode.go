// Package claudecode parses Claude Code session JSONL files under
// ~/.claude/projects. Behavior is a direct port of the Python provider:
// fake-prompt filtering, custom/ai titles, plan-file
// subtitles, fork detection, and the encoded-dir cwd resolution fix.
package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/QuietSugar/resumer/internal/session"
	"github.com/QuietSugar/resumer/internal/textutil"
)

const envProjectRoot = "RESUMER_CLAUDE_PROJECT_ROOT"

// Claude JSONL lines routinely exceed bufio's 64K default (tool results,
// base64 blobs). Without a bigger cap sessions silently vanish mid-parse.
const maxLineBytes = 10 << 20

var (
	branchRE = regexp.MustCompile(
		`(?s)Branched conversation.*?claude -r ` +
			`([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)
	planPathRE = regexp.MustCompile(`/\.claude/plans/[a-z0-9-]+\.md$`)
)

var planTitlePrefixes = []string{
	"Plan:", "plan:", "Research Plan:", "[OLD]", "[WIP]", "[DRAFT]",
}

var fakePromptPrefixes = []string{
	"<local-command",
	"<command-",
	"<system-reminder",
	"<ide_opened_file",
	"Caveat:",
	"[Request interrupted",
	"Base directory for this skill",
}

func projectRoot() string {
	if v := os.Getenv(envProjectRoot); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "projects")
}

// Provider implements provider.Provider for Claude Code.
type Provider struct {
	planTitleCache map[string]string
	planTitleSeen  map[string]bool
}

func New() *Provider {
	return &Provider{
		planTitleCache: map[string]string{},
		planTitleSeen:  map[string]bool{},
	}
}

func (p *Provider) Name() string      { return "claude-code" }
func (p *Provider) Badge() string     { return "cc" }
func (p *Provider) BadgeANSI() string { return "\x1b[32m" } // green

func (p *Provider) IsAvailable() bool {
	st, err := os.Stat(projectRoot())
	return err == nil && st.IsDir()
}

func (p *Provider) readPlanTitle(path string) string {
	if p.planTitleSeen[path] {
		return p.planTitleCache[path]
	}
	title := ""
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for i := 0; i < 20 && sc.Scan(); i++ {
			stripped := strings.TrimSpace(sc.Text())
			if stripped == "" {
				continue
			}
			if strings.HasPrefix(stripped, "#") {
				text := strings.TrimSpace(strings.TrimLeft(stripped, "#"))
				for _, prefix := range planTitlePrefixes {
					if strings.HasPrefix(text, prefix) {
						text = strings.TrimSpace(text[len(prefix):])
					}
				}
				if runes := []rune(text); len(runes) > 60 {
					text = string(runes[:59]) + "…"
				}
				title = text
				break
			}
		}
		f.Close()
	}
	p.planTitleSeen[path] = true
	p.planTitleCache[path] = title
	return title
}

func isRealPrompt(txt string) bool {
	s := strings.TrimSpace(txt)
	if s == "" {
		return false
	}
	for _, prefix := range fakePromptPrefixes {
		if strings.HasPrefix(s, prefix) {
			return false
		}
	}
	return true
}

type contentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Name  string `json:"name"`
	Input *struct {
		FilePath string `json:"file_path"`
	} `json:"input"`
}

type messageBody struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type record struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Snapshot  *struct {
		Timestamp string `json:"timestamp"`
	} `json:"snapshot"`
	Cwd         string          `json:"cwd"`
	Subtype     string          `json:"subtype"`
	Content     json.RawMessage `json:"content"`
	Message     json.RawMessage `json:"message"`
	CustomTitle string          `json:"customTitle"`
	AITitle     string          `json:"aiTitle"`
}

// extractUserText mirrors the Python helper: plain string content, or the
// newline-join of text blocks in a content list.
func extractUserText(raw json.RawMessage) string {
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
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func (p *Provider) parseJSONL(path string) *session.Session {
	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	encodedDir := filepath.Base(filepath.Dir(path))

	var firstTS, lastTS, cwd string
	asstCount := 0
	var prompts []session.Prompt
	forkedFrom := ""
	forkChecked := false
	planPath := ""
	customTitle := ""
	aiTitle := ""

	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	for sc.Scan() {
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue
		}
		ts := r.Timestamp
		if ts == "" && r.Snapshot != nil {
			ts = r.Snapshot.Timestamp
		}
		if ts != "" {
			if firstTS == "" {
				firstTS = ts
			}
			lastTS = ts
		}
		switch r.Type {
		case "system":
			if cwd == "" {
				cwd = r.Cwd
			}
			if !forkChecked && r.Subtype == "local_command" {
				var content string
				_ = json.Unmarshal(r.Content, &content)
				if strings.Contains(content, "Branched conversation") {
					if m := branchRE.FindStringSubmatch(content); m != nil {
						forkedFrom = m[1]
						forkChecked = true
					}
				}
			}
		case "message", "assistant":
			var msg messageBody
			_ = json.Unmarshal(r.Message, &msg)
			role := msg.Role
			if role == "" && r.Type == "assistant" {
				role = "assistant"
			}
			if role == "assistant" {
				asstCount++
			}
			var blocks []contentBlock
			if err := json.Unmarshal(msg.Content, &blocks); err == nil {
				for _, b := range blocks {
					if b.Type != "tool_use" {
						continue
					}
					if b.Name != "Write" && b.Name != "Edit" && b.Name != "MultiEdit" {
						continue
					}
					if b.Input != nil && planPathRE.MatchString(b.Input.FilePath) {
						planPath = b.Input.FilePath
					}
				}
			}
		case "user":
			var msg messageBody
			_ = json.Unmarshal(r.Message, &msg)
			text := extractUserText(msg.Content)
			if isRealPrompt(text) {
				prompts = append(prompts, session.Prompt{
					TS:   r.Timestamp,
					Text: strings.TrimSpace(text),
				})
			}
		case "custom-title":
			if v := strings.TrimSpace(r.CustomTitle); v != "" {
				customTitle = v
			}
		case "ai-title":
			if v := strings.TrimSpace(r.AITitle); v != "" {
				aiTitle = v
			}
		}
	}

	// Project label from the session's real cwd (basename), falling back to
	// the encoded dir's last hyphen segment for legacy files without cwd.
	projectLabel := "(unknown)"
	if cwd != "" {
		if base := filepath.Base(strings.TrimRight(cwd, "/")); base != "" && base != "/" && base != "." {
			projectLabel = base
		}
	} else {
		trimmed := strings.TrimLeft(encodedDir, "-")
		if idx := strings.LastIndex(trimmed, "-"); idx >= 0 {
			trimmed = trimmed[idx+1:]
		}
		if trimmed != "" {
			projectLabel = trimmed
		}
	}

	planTitle := ""
	if planPath != "" {
		planTitle = p.readPlanTitle(planPath)
	}

	title := customTitle
	if title == "" {
		title = aiTitle
	}
	var subtitleParts []string
	if planTitle != "" {
		subtitleParts = append(subtitleParts, planTitle)
	}
	if forkedFrom != "" {
		subtitleParts = append(subtitleParts, fmt.Sprintf("forked from %s", forkedFrom[:8]))
	}
	subtitle := strings.Join(subtitleParts, " · ")

	firstPrompt, lastPrompt := "", ""
	if len(prompts) > 0 {
		firstPrompt = prompts[0].Text
		lastPrompt = prompts[len(prompts)-1].Text
	}
	return &session.Session{
		Source:       "claude-code",
		SessionID:    sessionID,
		Path:         path,
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
		ResumeArgv:   []string{"claude", "--resume", sessionID},
	}
}

func findSessionFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == "subagents" {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".jsonl") {
			out = append(out, path)
		}
		return nil
	})
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
	root := projectRoot()
	cutoff := cutoffForFilters(f)
	var day *time.Time
	if f.Date != "" {
		if d, err := time.ParseInLocation("2006-01-02", f.Date, time.UTC); err == nil {
			day = &d
		}
	}

	var out []session.Session
	for _, path := range findSessionFiles(root) {
		s := p.parseJSONL(path)
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
	for _, path := range findSessionFiles(projectRoot()) {
		if strings.TrimSuffix(filepath.Base(path), ".jsonl") == id {
			return p.parseJSONL(path), nil
		}
	}
	return nil, nil
}
