package tui

import (
	"os"
	"path/filepath"
	"strings"
)

// customTipsPath resolves the optional user-defined tips file. The environment
// variable takes precedence over the standard per-user config location.
func customTipsPath() string {
	if path := strings.TrimSpace(os.Getenv("RESUMER_TIPS_FILE")); path != "" {
		if path == "~" || strings.HasPrefix(path, "~"+string(os.PathSeparator)) {
			home, err := os.UserHomeDir()
			if err == nil {
				if path == "~" {
					path = home
				} else {
					path = filepath.Join(home, strings.TrimPrefix(path, "~"+string(os.PathSeparator)))
				}
			}
		}
		return path
	}

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "resumer", "tips.md")
}

// loadCustomTips reads the optional plain-text tips file once when the picker
// starts. An unset, missing, unreadable, or blank file falls back to built-in
// provider tips.
func loadCustomTips() string {
	path := customTipsPath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// tipsForSource returns the built-in tips text for a provider. Custom tips
// (tips.md) replace this content in the help popup when present.
func tipsForSource(source string) string {
	lines := []string{
		"Session tips",
		"resumer only browses and resumes.",
		"",
	}
	switch source {
	case "opencode":
		lines = append(lines,
			"OpenCode: delete with",
			"opencode session delete",
			"<sessionID>",
		)
	case "kimi-code":
		lines = append(lines,
			"Kimi Code: open its session",
			"picker, select a session,",
			"press Ctrl+X, then confirm.",
		)
	case "codebuddy":
		lines = append(lines,
			"CodeBuddy",
			"Switch: /resume <session-id>",
			"Picker: codebuddy --resume",
			"Latest: codebuddy --continue",
			"Rename: /rename <name>",
			"New: /clear (history stays)",
			"Delete one: Beta HTTP API",
			"DELETE /api/v1/sessions/:id",
			"Project purge is broader.",
			"Preview purge: --dry-run.",
		)
	default:
		lines = append(lines,
			"Use this Agent's own CLI/TUI",
			"session manager. The exact",
			"steps differ by Agent/version.",
		)
	}
	lines = append(lines,
		"",
		"Check the session ID first.",
		"Deletion may be irreversible.",
	)
	return strings.Join(lines, "\n")
}
