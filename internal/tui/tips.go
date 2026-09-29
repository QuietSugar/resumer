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
