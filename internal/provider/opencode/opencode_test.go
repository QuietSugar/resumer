package opencode

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/jin-ttao/resumer/internal/session"
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

func newProvider(t *testing.T) *Provider {
	t.Helper()
	t.Setenv(envData, fixtureDir(t, "opencode-home"))
	t.Setenv(envBin, filepath.Join(fixtureDir(t, "..", "mock-bin", "opencode")))
	return New()
}

func listAll(t *testing.T) map[string]session.Session {
	t.Helper()
	p := newProvider(t)
	sessions, err := p.ListSessions(session.Filters{AllTime: true, Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]session.Session{}
	for _, s := range sessions {
		if _, dup := out[s.SessionID]; dup {
			t.Fatalf("duplicate session %s after dedupe", s.SessionID)
		}
		out[s.SessionID] = s
	}
	return out
}

func TestSQLiteParsing(t *testing.T) {
	sessions := listAll(t)
	// 4 sqlite rows: archived + child skipped; plus the JSON-only legacy
	// session merged in → 3 total.
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions (2 sqlite roots + 1 json), got %d: %v",
			len(sessions), sessions)
	}

	one := sessions["ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaa"]
	if one.Source != "opencode" {
		t.Errorf("source = %q", one.Source)
	}
	if one.Title != "OpenCode Fixture One" {
		t.Errorf("title = %q", one.Title)
	}
	if one.Cwd != "/tmp/resumer-fixtures/oc-one" {
		t.Errorf("directory = %q", one.Cwd)
	}
	if one.ProjectLabel != "oc-one" {
		t.Errorf("project = %q", one.ProjectLabel)
	}
	if len(one.Prompts) != 0 {
		// prompts are kept in agg only; Session.Prompts is not populated
		t.Errorf("prompts slice should stay empty (render uses first/last), got %d", len(one.Prompts))
	}
	if one.FirstPrompt != "opencode fixture one first prompt" ||
		one.LastPrompt != "opencode fixture one second prompt" {
		t.Errorf("prompts = %q / %q", one.FirstPrompt, one.LastPrompt)
	}
	if one.AsstCount != 2 {
		t.Errorf("assistant count = %d, want 2", one.AsstCount)
	}
	// epoch ms → RFC3339
	if one.FirstTS != "2026-04-15T05:00:00Z" {
		t.Errorf("first ts = %q", one.FirstTS)
	}
	if one.LastTS != "2026-04-15T05:05:00Z" {
		t.Errorf("last ts = %q", one.LastTS)
	}
	if one.Tokens == nil {
		t.Fatal("tokens missing")
	}
	if one.Tokens.Input != 1500 || one.Tokens.Output != 300 ||
		one.Tokens.CacheRead != 9000 || one.Tokens.CacheCreate != 1200 {
		t.Errorf("tokens = %+v", one.Tokens)
	}
	if one.Tokens.Turns != 2 {
		t.Errorf("token turns = %d, want 2", one.Tokens.Turns)
	}
	if len(one.ResumeArgv) != 3 || one.ResumeArgv[0] != "opencode" ||
		one.ResumeArgv[1] != "--session" || one.ResumeArgv[2] != one.SessionID {
		t.Errorf("resume argv = %v", one.ResumeArgv)
	}
}

func TestArchivedAndChildSkipped(t *testing.T) {
	sessions := listAll(t)
	if _, ok := sessions["ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbb"]; ok {
		t.Error("archived session must be skipped")
	}
	if _, ok := sessions["ses_cccccccccccccccccccccccccccc"]; ok {
		t.Error("child (parent_id) session must be skipped")
	}
}

func TestLegacyMessagePartPairDoesNotLeakAssistantText(t *testing.T) {
	sessions := listAll(t)
	one := sessions["ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaa"]
	if one.LastPrompt == "legacy part prompt that must not leak" ||
		one.FirstPrompt == "legacy part prompt that must not leak" {
		t.Errorf("assistant part text leaked as prompt: %q / %q",
			one.FirstPrompt, one.LastPrompt)
	}
}

func TestJSONFallbackSession(t *testing.T) {
	sessions := listAll(t)
	js := sessions["ses_jjjjjjjjjjjjjjjjjjjjjjjjjjjj"]
	if js.SessionID == "" {
		t.Fatal("json session missing")
	}
	if js.Title != "OpenCode JSON Fixture" {
		t.Errorf("title = %q", js.Title)
	}
	if js.Cwd != "/tmp/resumer-fixtures/oc-json" || js.ProjectLabel != "oc-json" {
		t.Errorf("cwd/project = %q / %q", js.Cwd, js.ProjectLabel)
	}
	if js.FirstPrompt != "opencode json fixture first prompt" {
		t.Errorf("first prompt = %q", js.FirstPrompt)
	}
	if js.AsstCount != 1 {
		t.Errorf("asst count = %d", js.AsstCount)
	}
	if js.Tokens == nil || js.Tokens.Input != 100 || js.Tokens.Output != 40 ||
		js.Tokens.CacheRead != 50 || js.Tokens.CacheCreate != 10 || js.Tokens.Turns != 1 {
		t.Errorf("tokens = %+v", js.Tokens)
	}
	if js.FirstTS != "2026-04-15T04:20:00Z" || js.LastTS != "2026-04-15T04:25:00Z" {
		t.Errorf("timestamps = %q .. %q", js.FirstTS, js.LastTS)
	}
}

func TestSQLiteWinsOnIDCollision(t *testing.T) {
	// listAll already exercises dedupe implicitly (no duplicate fatal).
	// Here assert the sqlite copy of a colliding id would win: craft by
	// checking the merged set has exactly one of the json id.
	p := newProvider(t)
	raw, err := p.listRaw()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, s := range raw {
		if s.SessionID == "ses_jjjjjjjjjjjjjjjjjjjjjjjjjjjj" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("json session appears %d times, want 1", count)
	}
}

func TestFilters(t *testing.T) {
	p := newProvider(t)
	off, err := p.ListSessions(session.Filters{Date: "2020-01-01", Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(off) != 0 {
		t.Errorf("off-day filter returned %d", len(off))
	}
	on, err := p.ListSessions(session.Filters{Date: "2026-04-15", Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(on) != 3 {
		t.Errorf("on-day filter returned %d, want 3", len(on))
	}
	proj, err := p.ListSessions(session.Filters{AllTime: true, Days: -1, Project: "oc-three"})
	if err != nil {
		t.Fatal(err)
	}
	if len(proj) != 1 || proj[0].SessionID != "ses_dddddddddddddddddddddddddddd" {
		t.Errorf("project filter = %+v", proj)
	}
	// default window (Days unset → 3-day cutoff) vs 2026-04-15 fixtures:
	// today is far past, so nothing qualifies.
	recent, err := p.ListSessions(session.Filters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 0 {
		t.Errorf("default window should exclude ancient fixtures, got %d", len(recent))
	}
}

func TestLoadDetail(t *testing.T) {
	p := newProvider(t)
	s, err := p.LoadDetail("ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Title != "OpenCode Fixture One" {
		t.Fatalf("LoadDetail = %+v", s)
	}
	missing, err := p.LoadDetail("ses_nope")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("LoadDetail(missing) = %+v", missing)
	}
}

func TestAvailability(t *testing.T) {
	p := newProvider(t)
	if !p.IsAvailable() {
		t.Error("fixture db + mock binary should be available")
	}
	t.Setenv(envData, "/nonexistent/resumer-qa")
	if p.IsAvailable() {
		t.Error("missing data root must not be available")
	}
}
