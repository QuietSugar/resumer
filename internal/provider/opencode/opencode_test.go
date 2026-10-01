package opencode

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	// 5 sqlite rows → archived + child skipped, 3 sqlite roots remain. The
	// JSON-only legacy session is not merged in: a database is the source of
	// truth (see TestLegacyJSONIgnoredWhenDatabaseExists).
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions (3 sqlite roots), got %d: %v",
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
	// ses_aaaa is dual-projected (session_message + legacy message/part hold
	// the same two turns, as opencode 1.18.x writes both). The legacy copies
	// must collapse into the durable ones: 2 turns, not 4.
	if one.AsstCount != 2 {
		t.Errorf("assistant count = %d, want 2 (dual-write dedupe failed)", one.AsstCount)
	}
	// epoch ms → RFC3339
	if one.FirstTS != "2026-04-15T05:00:00Z" {
		t.Errorf("first ts = %q", one.FirstTS)
	}
	if one.LastTS != "2026-04-15T05:05:00Z" {
		t.Errorf("last ts = %q", one.LastTS)
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

// Sessions that predate an upgrade exist only in the legacy message+part
// pair — opencode ships no backfill migration for them, so the provider must
// surface them from the v1 tables alone.
func TestLegacyOnlySession(t *testing.T) {
	sessions := listAll(t)
	leg := sessions["ses_eeeeeeeeeeeeeeeeeeeeeeeeeeee"]
	if leg.SessionID == "" {
		t.Fatal("legacy-only session missing")
	}
	if leg.Title != "OpenCode Legacy Session" {
		t.Errorf("title = %q", leg.Title)
	}
	if leg.Cwd != "/tmp/resumer-fixtures/oc-two" || leg.ProjectLabel != "oc-two" {
		t.Errorf("cwd/project = %q / %q", leg.Cwd, leg.ProjectLabel)
	}
	if leg.FirstPrompt != "legacy only first prompt" ||
		leg.LastPrompt != "legacy only second prompt" {
		t.Errorf("prompts = %q / %q", leg.FirstPrompt, leg.LastPrompt)
	}
	if strings.Contains(leg.FirstPrompt+leg.LastPrompt, "synthetic legacy prompt") {
		t.Error("synthetic part text leaked as prompt")
	}
	if leg.AsstCount != 1 {
		t.Errorf("assistant count = %d, want 1 (tool-call continuation should stay in the same turn)", leg.AsstCount)
	}
	if leg.FirstTS != "2026-04-15T06:00:00Z" || leg.LastTS != "2026-04-15T06:05:00Z" {
		t.Errorf("timestamps = %q .. %q", leg.FirstTS, leg.LastTS)
	}
}

// The pre-1.1 JSON store is a fallback for installs that predate the database,
// so it is exercised from a store that has no opencode.db at all.
func TestJSONFallbackSession(t *testing.T) {
	store := filepath.Join(t.TempDir(), "opencode-home")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	copyTree(t, fixtureDir(t, "opencode-home", "storage"), filepath.Join(store, "storage"))
	t.Setenv(envData, store)
	t.Setenv(envBin, filepath.Join(fixtureDir(t, "..", "mock-bin", "opencode")))

	p := New()
	all, err := p.ListSessions(session.Filters{AllTime: true, Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("db-less store listed %d sessions, want 1 (the JSON-only one)", len(all))
	}
	sessions := map[string]session.Session{}
	for _, s := range all {
		sessions[s.SessionID] = s
	}
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
	if js.FirstTS != "2026-04-15T04:20:00Z" || js.LastTS != "2026-04-15T04:25:00Z" {
		t.Errorf("timestamps = %q .. %q", js.FirstTS, js.LastTS)
	}
}

// A database is the only source of truth when it exists, so the legacy JSON
// store is not consulted beside one — not even for a session the database has
// never heard of. opencode empties storage/session/ when it moves to the
// database, so a surviving JSON file is a leftover, and resurrecting from it
// would bring back sessions the user deleted or archived.
func TestLegacyJSONIgnoredWhenDatabaseExists(t *testing.T) {
	p := newProvider(t)
	raw, err := p.listRaw()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, s := range raw {
		seen[s.SessionID]++
	}
	if n := seen["ses_jjjjjjjjjjjjjjjjjjjjjjjjjjjj"]; n != 0 {
		t.Errorf("json-only session listed %d times beside a database, want 0", n)
	}
	for _, id := range []string{
		"ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"ses_dddddddddddddddddddddddddddd",
		"ses_eeeeeeeeeeeeeeeeeeeeeeeeeeee",
	} {
		if seen[id] != 1 {
			t.Errorf("database session %s listed %d times, want 1", id, seen[id])
		}
	}
	if len(seen) != 3 {
		t.Errorf("listed %d sessions, want the 3 database roots", len(seen))
	}
}

// A leftover JSON file for a session the database skips as archived must not
// resurrect it: with a database present the JSON store is not read at all.
func TestArchivedSessionNotResurrectedFromJSON(t *testing.T) {
	store := filepath.Join(t.TempDir(), "opencode-home")
	copyTree(t, fixtureDir(t, "opencode-home"), store)

	legacy := filepath.Join(store, "storage", "session", "proj-archived")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	leftover := `{
  "id": "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "projectID": "proj-archived",
  "directory": "/tmp/resumer-fixtures/oc-archived",
  "title": "OpenCode Archived Leftover",
  "version": "1.0.0",
  "time": { "created": 1776228000000, "updated": 1776229000000 }
}`
	if err := os.WriteFile(filepath.Join(legacy, "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbb.json"),
		[]byte(leftover), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv(envData, store)
	p := New()
	sessions, err := p.ListSessions(session.Filters{AllTime: true, Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if s.SessionID == "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
			t.Errorf("archived session resurrected from the legacy JSON store: %q", s.Title)
		}
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
	proj2, err := p.ListSessions(session.Filters{AllTime: true, Days: -1, Project: "oc-two"})
	if err != nil {
		t.Fatal(err)
	}
	// oc-two matches the legacy-only session; the archived one is skipped.
	if len(proj2) != 1 || proj2[0].SessionID != "ses_eeeeeeeeeeeeeeeeeeeeeeeeeeee" {
		t.Errorf("project filter oc-two = %+v", proj2)
	}
	// An explicit narrow window vs the 2026-04-15 fixtures: today is far past,
	// so nothing qualifies. (The zero value no longer implies a window —
	// Days == 0 means "no time limit" and is the CLI default.)
	recent, err := p.ListSessions(session.Filters{Days: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 0 {
		t.Errorf("3-day window should exclude ancient fixtures, got %d", len(recent))
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

// TestNoTimeFilterByDefault locks in the CLI default: Days == 0 (the zero
// value) must impose no time limit. The fixtures are dated 2026-04-15, far
// outside any recent window, so a stale default would silently drop them all.
func TestNoTimeFilterByDefault(t *testing.T) {
	p := newProvider(t)
	sessions, err := p.ListSessions(session.Filters{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Fatalf("zero-value filter listed %d sessions, want 3 (no time limit)", len(sessions))
	}
}
