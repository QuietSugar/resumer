package claudecode

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/QuietSugar/resumer/internal/session"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "tests", "fixtures", "claude-code")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// copyTree copies a fixture into a temp directory so a test can mutate it
// without touching what `go test ./...` reads.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func listAll(t *testing.T) map[string]session.Session {
	t.Helper()
	t.Setenv("RESUMER_CLAUDE_PROJECT_ROOT", fixtureRoot(t))
	p := New()
	sessions, err := p.ListSessions(session.Filters{AllTime: true, Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]session.Session{}
	for _, s := range sessions {
		out[s.SessionID] = s
	}
	return out
}

func TestFixtureParsing(t *testing.T) {
	sessions := listAll(t)
	if len(sessions) != 5 {
		t.Fatalf("expected 5 fixture sessions, got %d", len(sessions))
	}

	plain := sessions["aaaaaaaa-0001-4000-8000-000000000001"]
	if plain.ProjectLabel != "alpha" {
		t.Errorf("plain project = %q", plain.ProjectLabel)
	}
	if plain.FirstPrompt != "plain fixture first prompt" {
		t.Errorf("plain first prompt = %q", plain.FirstPrompt)
	}
	if plain.LastPrompt != "plain fixture second prompt" {
		t.Errorf("plain last prompt = %q", plain.LastPrompt)
	}
	if plain.AsstCount != 2 || len(plain.Prompts) != 2 {
		t.Errorf("plain counts: asst=%d prompts=%d", plain.AsstCount, len(plain.Prompts))
	}
	if plain.FirstTS != "2026-04-15T01:00:00.000Z" || plain.LastTS != "2026-04-15T01:05:07.000Z" {
		t.Errorf("plain ts = %q .. %q", plain.FirstTS, plain.LastTS)
	}
	wantArgv := []string{"claude", "--resume", "aaaaaaaa-0001-4000-8000-000000000001"}
	if strings.Join(plain.ResumeArgv, " ") != strings.Join(wantArgv, " ") {
		t.Errorf("plain argv = %v", plain.ResumeArgv)
	}

	titled := sessions["aaaaaaaa-0002-4000-8000-000000000002"]
	if titled.Title != "Custom Titled Session" {
		t.Errorf("custom title wins: %q", titled.Title)
	}

	aiTitled := sessions["bbbbbbbb-0003-4000-8000-000000000003"]
	if aiTitled.Title != "Auto Generated Title" {
		t.Errorf("ai title = %q", aiTitled.Title)
	}
	if aiTitled.FirstPrompt != "ai-titled fixture prompt" {
		t.Errorf("content-list prompt extraction = %q", aiTitled.FirstPrompt)
	}

	forked := sessions["bbbbbbbb-0004-4000-8000-000000000004"]
	if forked.Subtitle != "forked from aaaaaaaa" {
		t.Errorf("fork subtitle = %q", forked.Subtitle)
	}
	if len(forked.Prompts) != 1 || forked.Prompts[0].Text != "forked session continues here" {
		t.Errorf("forked prompts = %+v", forked.Prompts)
	}

	stale := sessions["dddddddd-0006-4000-8000-000000000006"]
	if stale.Cwd != "/bogus/wrong/path" {
		t.Errorf("stale cwd = %q", stale.Cwd)
	}
	if stale.ProjectLabel != "path" {
		t.Errorf("stale project label = %q", stale.ProjectLabel)
	}
}

func TestProjectFilter(t *testing.T) {
	t.Setenv("RESUMER_CLAUDE_PROJECT_ROOT", fixtureRoot(t))
	p := New()
	sessions, err := p.ListSessions(session.Filters{AllTime: true, Days: -1, Project: "ALPHA"})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("case-insensitive project filter: got %d, want 2", len(sessions))
	}
}

func TestDateFilter(t *testing.T) {
	t.Setenv("RESUMER_CLAUDE_PROJECT_ROOT", fixtureRoot(t))
	p := New()
	on, err := p.ListSessions(session.Filters{Date: "2026-04-15", Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != 5 {
		t.Errorf("date filter on fixture day: got %d, want 5", len(on))
	}
	off, err := p.ListSessions(session.Filters{Date: "2020-01-01", Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != 0 {
		t.Errorf("date filter off-day: got %d, want 0", len(off))
	}
}

// writeSession ports the Python test helper: minimal JSONL under an encoded dir.
func writeSession(t *testing.T, parent, encodedDir, cwd string, includeCwd bool) string {
	t.Helper()
	dir := filepath.Join(parent, encodedDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "aaaaaaaa-0001-4000-8000-000000000001.jsonl")
	sysRec := map[string]any{
		"type": "system", "subtype": "init", "timestamp": "2026-04-15T01:00:00.000Z",
	}
	if includeCwd {
		sysRec["cwd"] = cwd
	}
	userRec := map[string]any{
		"type": "user", "timestamp": "2026-04-15T01:00:05.000Z",
		"message": map[string]any{"role": "user", "content": "hi"},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	_ = enc.Encode(sysRec)
	_ = enc.Encode(userRec)
	return path
}

func TestProjectLabel(t *testing.T) {
	tmp := t.TempDir()
	p := New()

	cases := []struct {
		name       string
		encodedDir string
		cwd        string
		includeCwd bool
		want       string
	}{
		{"cwd basename simple", "-Users-alice-Desktop-myrepo", "/Users/alice/Desktop/myrepo", true, "myrepo"},
		{"cwd basename other user", "-Users-bob-projects-foo", "/Users/bob/projects/foo", true, "foo"},
		{"icloud obsidian", "-Users-alice-Library-Mobile-Documents-iCloud-md-obsidian-Documents-tao",
			"/Users/alice/Library/Mobile Documents/iCloud~md~obsidian/Documents/tao", true, "tao"},
		{"trailing slash", "-tmp-resumer-test-x", "/tmp/resumer-test/x/", true, "x"},
		{"missing cwd falls back to encoded segment", "-some-encoded-myproject", "", false, "myproject"},
		{"encoded empty → unknown", "-", "", false, "(unknown)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(tmp, strings.ReplaceAll(c.name, " ", "_"))
			path := writeSession(t, dir, c.encodedDir, c.cwd, c.includeCwd)
			s := p.parseJSONL(path)
			if s == nil {
				t.Fatal("parse returned nil")
			}
			if s.ProjectLabel != c.want {
				t.Errorf("project_label = %q, want %q", s.ProjectLabel, c.want)
			}
		})
	}
}

func TestLongLineSurvivesParsing(t *testing.T) {
	// Real Claude JSONL lines exceed bufio's 64K default. A session whose
	// line blows the default buffer must still parse (regression guard for
	// the enlarged Scanner buffer).
	tmp := t.TempDir()
	big := strings.Repeat("x", 200<<10) // 200KB single line payload
	dir := filepath.Join(tmp, "-tmp-bigline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "eeeeeeee-0001-4000-8000-000000000001.jsonl")
	content := `{"type":"system","subtype":"init","cwd":"/tmp/bigline","timestamp":"2026-04-15T01:00:00.000Z"}` + "\n" +
		`{"type":"user","timestamp":"2026-04-15T01:00:05.000Z","message":{"role":"user","content":"` + big + `"}}` + "\n" +
		`{"type":"user","timestamp":"2026-04-15T01:00:10.000Z","message":{"role":"user","content":"after the big line"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New().parseJSONL(path)
	if s == nil {
		t.Fatal("parse returned nil")
	}
	if len(s.Prompts) != 2 {
		t.Fatalf("prompts after long line = %d, want 2", len(s.Prompts))
	}
	if s.LastPrompt != "after the big line" {
		t.Errorf("line after the 200KB one was lost: %q", s.LastPrompt)
	}
}

// TestNoTimeFilterByDefault locks in the CLI default: Days == 0 (the zero
// value) must impose no time limit. The fixtures are dated 2026-04-15, far
// outside any recent window, so a stale default would silently drop them all.
func TestNoTimeFilterByDefault(t *testing.T) {
	t.Setenv("RESUMER_CLAUDE_PROJECT_ROOT", fixtureRoot(t))
	p := New()
	sessions, err := p.ListSessions(session.Filters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 5 {
		t.Fatalf("zero-value filter listed %d sessions, want 5 (no time limit)", len(sessions))
	}
}

// Sub-agent traffic lands in the same file as the main conversation, flagged
// with isSidechain. It is not part of this session, so it must not inflate the
// prompt list, the assistant-turn estimate, or the activity window.
func TestSidechainRecordsDoNotCount(t *testing.T) {
	root := filepath.Join(t.TempDir(), "claude-code")
	copyTree(t, fixtureRoot(t), root)

	target := filepath.Join(root, "-fixture-alpha", "aaaaaaaa-0001-4000-8000-000000000001.jsonl")
	f, err := os.OpenFile(target, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	// Timestamps after the session's own last record, so a leak would also
	// move the activity window.
	sidechain := `{"type":"user","timestamp":"2026-04-15T02:00:00.000Z","isSidechain":true,"message":{"role":"user","content":"SUBAGENT PROMPT MUST NOT COUNT"}}
{"type":"assistant","timestamp":"2026-04-15T02:00:05.000Z","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent reply"}]}}
`
	if _, err := f.WriteString(sidechain); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RESUMER_CLAUDE_PROJECT_ROOT", root)
	p := New()
	sessions, err := p.ListSessions(session.Filters{AllTime: true, Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	var one *session.Session
	for i := range sessions {
		if sessions[i].SessionID == "aaaaaaaa-0001-4000-8000-000000000001" {
			one = &sessions[i]
		}
	}
	if one == nil {
		t.Fatal("session aaaaaaaa-0001 missing")
	}
	if len(one.Prompts) != 2 {
		t.Errorf("prompts = %d, want 2 — a sidechain user record was counted", len(one.Prompts))
	}
	if one.AsstCount != 2 {
		t.Errorf("asst count = %d, want 2 — a sidechain assistant record was counted", one.AsstCount)
	}
	if one.LastTS != "2026-04-15T01:05:07.000Z" {
		t.Errorf("last ts = %q, want the main session's own 2026-04-15T01:05:07.000Z", one.LastTS)
	}
	for _, pr := range one.Prompts {
		if strings.Contains(pr.Text, "SUBAGENT") {
			t.Errorf("sidechain prompt leaked into the prompt list: %q", pr.Text)
		}
	}
}
