// Package session defines the provider-agnostic session schema shared by
// providers, renderers, the TUI, and the exec dispatcher.
package session

// Prompt is one user prompt with its timestamp (empty string when unknown).
type Prompt struct {
	TS   string
	Text string
}

// Session is the common schema. String fields use "" for "absent" (the
// Python implementation used None); render/json restores explicit nulls.
type Session struct {
	Source       string
	SessionID    string
	Path         string
	ProjectLabel string
	Cwd          string
	FirstTS      string
	LastTS       string
	Title        string
	Subtitle     string
	FirstPrompt  string
	LastPrompt   string
	Prompts      []Prompt
	// AsstCount is a provider-defined estimate of assistant-side activity.
	// Providers may use different counting rules; the value can be inaccurate
	// and is intended only as a rough indication of conversation size, not an
	// exact or cross-provider comparable turn count.
	AsstCount  int
	ResumeArgv []string
}

// Filters mirrors the CLI surface. Days < 0 means "unset" (provider default).
type Filters struct {
	Days    int
	Date    string
	AllTime bool
	Project string
	Limit   int
	Source  string
}
