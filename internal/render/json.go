package render

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/jin-ttao/resumer/internal/session"
)

// jsonPrompt and jsonSession mirror the provider-agnostic session schema.
type jsonPrompt struct {
	TS   string `json:"ts"`
	Text string `json:"text"`
}

type jsonSession struct {
	Source      string       `json:"source"`
	SessionID   string       `json:"session_id"`
	Path        string       `json:"path"`
	Cwd         *string      `json:"cwd"`
	FirstTS     *string      `json:"first_ts"`
	LastTS      *string      `json:"last_ts"`
	Title       *string      `json:"title"`
	Subtitle    *string      `json:"subtitle"`
	FirstPrompt *string      `json:"first_prompt"`
	LastPrompt  *string      `json:"last_prompt"`
	AsstCount   int          `json:"asst_count"`
	Prompts     []jsonPrompt `json:"prompts"`
	ResumeArgv  []string     `json:"resume_argv"`
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// JSON renders the session list as a 2-space-indented JSON array with HTML
// escaping off (parity with Python's ensure_ascii=False, indent=2).
func JSON(sessions []session.Session) string {
	out := make([]jsonSession, 0, len(sessions))
	for i := range sessions {
		s := &sessions[i]
		prompts := make([]jsonPrompt, 0, len(s.Prompts))
		for _, p := range s.Prompts {
			prompts = append(prompts, jsonPrompt{TS: p.TS, Text: p.Text})
		}
		argv := s.ResumeArgv
		if argv == nil {
			argv = []string{}
		}
		out = append(out, jsonSession{
			Source:      s.Source,
			SessionID:   s.SessionID,
			Path:        s.Path,
			Cwd:         nullable(s.Cwd),
			FirstTS:     nullable(s.FirstTS),
			LastTS:      nullable(s.LastTS),
			Title:       nullable(s.Title),
			Subtitle:    nullable(s.Subtitle),
			FirstPrompt: nullable(s.FirstPrompt),
			LastPrompt:  nullable(s.LastPrompt),
			AsstCount:   s.AsstCount,
			Prompts:     prompts,
			ResumeArgv:  argv,
		})
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return "[]"
	}
	return strings.TrimRight(buf.String(), "\n")
}
