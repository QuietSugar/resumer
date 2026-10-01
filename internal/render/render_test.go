package render

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuietSugar/resumer/internal/session"
	"github.com/QuietSugar/resumer/internal/textutil"
)

func TestFullBoxShowsFirstPromptWhenPromptListIsEmpty(t *testing.T) {
	box := FullBox(&session.Session{FirstPrompt: "Show this in the detail preview"})
	if !strings.Contains(box, "Show this in the detail preview") {
		t.Fatalf("detail box is missing first prompt:\n%s", box)
	}
}

func TestFullBoxLabelsAssistantActivityAsApproximate(t *testing.T) {
	box := FullBox(&session.Session{
		Prompts:   []session.Prompt{{Text: "one"}, {Text: "two"}},
		AsstCount: 7,
	})
	if !strings.Contains(box, "2 user prompts / ~7 assistant activity") {
		t.Fatalf("detail box does not mark assistant activity as approximate:\n%s", box)
	}
}

func TestRenderersOmitTokenAndCacheStatistics(t *testing.T) {
	sessions := []session.Session{{
		Source: "claude-code", SessionID: "fixture-session", ProjectLabel: "fixture",
	}}
	outputs := []string{
		Index(sessions),
		FullBox(&sessions[0]),
		JSON(sessions),
	}
	for _, output := range outputs {
		lower := strings.ToLower(output)
		if strings.Contains(lower, "token") || strings.Contains(lower, "cache hit") {
			t.Errorf("render output still contains usage statistics:\n%s", output)
		}
	}
}

func TestRenderersFlagMissingWorkingDirectory(t *testing.T) {
	gone := session.Session{
		Source: "kimi-code", SessionID: "s1", ProjectLabel: "old-project",
		Cwd: filepath.Join(t.TempDir(), "deleted-project"), FirstPrompt: "hello",
	}
	idx := Index([]session.Session{gone})
	// The project column must say the directory is gone, not pass the stale
	// name off as a live project.
	if !strings.Contains(idx, textutil.DirDeletedLabel) {
		t.Errorf("index row should mark the deleted directory:\n%s", idx)
	}
	if strings.Contains(idx, "old-project") {
		t.Errorf("index row should not show the stale project name:\n%s", idx)
	}
	box := FullBox(&gone)
	if !strings.Contains(box, "directory no longer exists") {
		t.Errorf("detail box should mark the deleted directory:\n%s", box)
	}
	if !strings.Contains(box, gone.Cwd) {
		t.Errorf("detail box should still show the missing path:\n%s", box)
	}

	alive := session.Session{
		Source: "kimi-code", SessionID: "s2", ProjectLabel: "live-project",
		Cwd: t.TempDir(), FirstPrompt: "hello",
	}
	if idx := Index([]session.Session{alive}); strings.Contains(idx, textutil.DirDeletedLabel) {
		t.Errorf("a session whose cwd exists must not be flagged:\n%s", idx)
	}
	if box := FullBox(&alive); strings.Contains(box, "directory no longer exists") {
		t.Errorf("a session whose cwd exists must not be flagged:\n%s", box)
	}
}
