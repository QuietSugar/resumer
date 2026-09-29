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
