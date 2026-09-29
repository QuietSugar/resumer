// Package config persists resumer's user settings. Today that is exactly one
// thing: which providers are disabled. A disabled provider is excluded from
// every ambient scan — resumer neither walks its storage nor parses its
// files — until it is turned back on.
//
// The file lives at $XDG_CONFIG_HOME/resumer/config.json (default
// ~/.config/resumer/config.json); $RESUMER_CONFIG overrides the whole path so
// tests and sandboxed runs can redirect it.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Config is the on-disk document. Field names are stable API: older resumer
// versions must keep reading newer files (unknown fields are ignored by
// json.Unmarshal) and vice versa.
type Config struct {
	// Disabled lists provider names that are turned off. Unknown names are
	// harmless and preserved, so a config written while a provider exists
	// still round-trips after an upgrade/downgrade.
	Disabled []string `json:"disabled,omitempty"`
}

// Path returns the config file location, honoring $RESUMER_CONFIG and
// $XDG_CONFIG_HOME.
func Path() string {
	if p := os.Getenv("RESUMER_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "resumer", "config.json")
}

// Load reads the config. A missing file yields the zero Config and a nil
// error. A malformed file yields an error — callers decide whether to fail
// hard (mutating commands) or degrade with a warning (read-only paths).
func Load() (Config, error) {
	var c Config
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("%s: %w", Path(), err)
	}
	return c, nil
}

// Save writes the config atomically (temp file + rename) so a crash cannot
// leave a half-written document behind.
func Save(c Config) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(p), ".config-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// IsDisabled reports whether name is in the disabled list.
func (c Config) IsDisabled(name string) bool {
	for _, d := range c.Disabled {
		if d == name {
			return true
		}
	}
	return false
}

// Disable adds name to the disabled list (idempotent, order-stable).
func (c *Config) Disable(name string) {
	if c.IsDisabled(name) {
		return
	}
	c.Disabled = append(c.Disabled, name)
	sort.Strings(c.Disabled)
}

// Enable removes name from the disabled list.
func (c *Config) Enable(name string) {
	out := c.Disabled[:0]
	for _, d := range c.Disabled {
		if d != name {
			out = append(out, d)
		}
	}
	c.Disabled = out
}
