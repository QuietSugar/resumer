package codebuddy

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jin-ttao/resumer/internal/session"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "tests", "fixtures", "codebuddy")
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestFixtureParsing(t *testing.T) {
	t.Setenv("RESUMER_CODEBUDDY_HOME", fixtureRoot(t))
	p := New()
	sessions, err := p.ListSessions(session.Filters{AllTime: true, Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}

	got := sessions[0]
	if got.Source != "codebuddy" || got.SessionID != "cb111111-1111-4111-8111-111111111111" {
		t.Errorf("identity = %q / %q", got.Source, got.SessionID)
	}
	if got.ProjectLabel != "codebuddy-alpha" || got.Cwd != "/tmp/resumer-fixtures/codebuddy-alpha" {
		t.Errorf("project/cwd = %q / %q", got.ProjectLabel, got.Cwd)
	}
	if got.Title != "CodeBuddy fixture session" {
		t.Errorf("title = %q", got.Title)
	}
	if got.FirstPrompt != "synthetic first prompt" || got.LastPrompt != "synthetic second prompt" || len(got.Prompts) != 2 {
		t.Errorf("prompts = %+v", got.Prompts)
	}
	if got.AsstCount != 1 {
		t.Errorf("assistant count = %d, want 1", got.AsstCount)
	}
	if got.FirstTS != "2026-04-15T05:00:00Z" || got.LastTS != "2026-04-15T05:05:07Z" {
		t.Errorf("timestamps = %q .. %q", got.FirstTS, got.LastTS)
	}
	wantArgv := []string{"codebuddy", "--resume", got.SessionID}
	if strings.Join(got.ResumeArgv, " ") != strings.Join(wantArgv, " ") {
		t.Errorf("resume argv = %v", got.ResumeArgv)
	}
}

func TestExtractTextContentBlockTypes(t *testing.T) {
	for _, typ := range []string{"text", "input_text", "output_text"} {
		t.Run(typ, func(t *testing.T) {
			got := extractText([]byte(`[ {"type":"` + typ + `","text":"hello"} ]`))
			if got != "hello" {
				t.Fatalf("extractText() = %q, want hello", got)
			}
		})
	}
}

func TestProjectAndDateFilters(t *testing.T) {
	t.Setenv("RESUMER_CODEBUDDY_HOME", fixtureRoot(t))
	p := New()

	matched, err := p.ListSessions(session.Filters{AllTime: true, Days: -1, Project: "BUDDY-ALPHA"})
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 1 {
		t.Fatalf("project filter returned %d sessions, want 1", len(matched))
	}

	missing, err := p.ListSessions(session.Filters{AllTime: true, Days: -1, Project: "does-not-match"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("non-matching project filter returned %d sessions", len(missing))
	}

	byDate, err := p.ListSessions(session.Filters{Date: "2026-04-15", Days: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(byDate) != 1 {
		t.Fatalf("date filter returned %d sessions, want 1", len(byDate))
	}
}

func TestAvailabilityAndDetailLookup(t *testing.T) {
	t.Setenv("RESUMER_CODEBUDDY_HOME", fixtureRoot(t))
	p := New()
	if !p.IsAvailable() {
		t.Fatal("fixture projects directory should be available")
	}
	got, err := p.LoadDetail("cb111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "CodeBuddy fixture session" {
		t.Fatalf("detail lookup = %+v", got)
	}
}
