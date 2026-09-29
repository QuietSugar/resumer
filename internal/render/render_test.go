package render

import (
	"strings"
	"testing"

	"github.com/jin-ttao/resumer/internal/session"
)

func TestFullBoxShowsFirstPromptWhenPromptListIsEmpty(t *testing.T) {
	box := FullBox(&session.Session{FirstPrompt: "Show this in the detail preview"})
	if !strings.Contains(box, "Show this in the detail preview") {
		t.Fatalf("detail box is missing first prompt:\n%s", box)
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
