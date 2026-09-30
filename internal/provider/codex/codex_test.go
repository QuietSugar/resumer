package codex

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/QuietSugar/resumer/internal/session"
)

func fixtureDir(t *testing.T, parts ...string) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(append([]string{filepath.Dir(thisFile), "..", "..", "..", "tests", "fixtures"}, parts...)...)
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func listAll(t *testing.T) map[string]session.Session {
	t.Helper()
	t.Setenv("RESUMER_CODEX_SESSION_ROOT", fixtureDir(t, "codex"))
	t.Setenv("RESUMER_CODEX_INDEX_FILE", fixtureDir(t, "codex", "session_index.jsonl"))
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
	// 4 rollout files exist; the one missing session_meta must be skipped.
	if len(sessions) != 3 {
		t.Fatalf("expected 3 valid codex sessions, got %d", len(sessions))
	}

	one := sessions["019cccc1-1111-7000-8000-000000000001"]
	if one.Cwd != "/tmp/resumer-fixtures/codex-one" {
		t.Errorf("cwd = %q", one.Cwd)
	}
	if one.ProjectLabel != "codex-one" {
		t.Errorf("project = %q", one.ProjectLabel)
	}
	if one.Title != "Codex Fixture One Thread" {
		t.Errorf("title from index = %q", one.Title)
	}
	if len(one.Prompts) != 2 ||
		one.FirstPrompt != "codex fixture one first prompt" ||
		one.LastPrompt != "codex fixture one second prompt" {
		t.Errorf("prompts = %+v", one.Prompts)
	}
	// 4 event_msg lines: task_started, 2× user_message, task_complete.
	if one.AsstCount != 4 {
		t.Errorf("event count = %d, want 4", one.AsstCount)
	}
	if one.FirstTS != "2026-04-15T05:00:00.000Z" {
		t.Errorf("first ts (from session_meta payload) = %q", one.FirstTS)
	}
	if len(one.ResumeArgv) != 3 || one.ResumeArgv[0] != "codex" || one.ResumeArgv[1] != "resume" {
		t.Errorf("resume argv = %v", one.ResumeArgv)
	}

	// response_item user-role injections (AGENTS.md etc.) must NOT count as
	// prompts — only event_msg/user_message does.
	two := sessions["019cccc2-2222-7000-8000-000000000002"]
	for _, p := range two.Prompts {
		if p.Text == "AGENTS.md injection that must be filtered out" {
			t.Error("response_item user message leaked into prompts")
		}
	}

	three := sessions["019cccc3-3333-7000-8000-000000000003"]
	if three.Title != "Codex Three Named" {
		t.Errorf("title = %q", three.Title)
	}

	if _, ok := sessions["019cccc4-4444-7000-8000-000000000004"]; ok {
		t.Error("rollout without session_meta must be skipped")
	}
}

func TestIndexGracefulOnMissing(t *testing.T) {
	t.Setenv("RESUMER_CODEX_SESSION_ROOT", fixtureDir(t, "codex"))
	t.Setenv("RESUMER_CODEX_INDEX_FILE", "/nonexistent/index.jsonl")
	p := New()
	sessions, err := p.ListSessions(session.Filters{AllTime: true, Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Fatalf("missing index must not drop sessions: got %d", len(sessions))
	}
	for _, s := range sessions {
		if s.Title != "" {
			t.Errorf("titles must be empty without index, got %q", s.Title)
		}
	}
}

func TestDateFilter(t *testing.T) {
	t.Setenv("RESUMER_CODEX_SESSION_ROOT", fixtureDir(t, "codex"))
	t.Setenv("RESUMER_CODEX_INDEX_FILE", fixtureDir(t, "codex", "session_index.jsonl"))
	p := New()
	off, err := p.ListSessions(session.Filters{Date: "2020-01-01", Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != 0 {
		t.Errorf("off-day date filter: got %d, want 0", len(off))
	}
	on, err := p.ListSessions(session.Filters{Date: "2026-04-15", Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != 3 {
		t.Errorf("on-day date filter: got %d, want 3", len(on))
	}
}
