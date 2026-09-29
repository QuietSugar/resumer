package config

import (
	"os"
	"path/filepath"
	"testing"
)

func setPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "resumer", "config.json")
	t.Setenv("RESUMER_CONFIG", p)
	return p
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	setPath(t)
	c, err := Load()
	if err != nil {
		t.Fatalf("Load missing file: %v", err)
	}
	if len(c.Disabled) != 0 {
		t.Errorf("expected zero config, got %+v", c)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := setPath(t)
	var c Config
	c.Disable("opencode")
	c.Disable("codex")
	c.Disable("opencode") // idempotent
	if err := Save(c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Disabled) != 2 || got.Disabled[0] != "codex" || got.Disabled[1] != "opencode" {
		t.Errorf("round-trip = %+v", got.Disabled)
	}
	if !got.IsDisabled("opencode") || got.IsDisabled("claude-code") {
		t.Errorf("IsDisabled wrong: %+v", got)
	}
	// Parent dirs were created on demand.
	if _, err := os.Stat(filepath.Dir(p)); err != nil {
		t.Errorf("parent dir: %v", err)
	}
}

func TestEnableRemovesEntry(t *testing.T) {
	var c Config
	c.Disable("opencode")
	c.Disable("codex")
	c.Enable("opencode")
	if c.IsDisabled("opencode") || !c.IsDisabled("codex") {
		t.Errorf("after Enable: %+v", c.Disabled)
	}
	c.Enable("not-there") // no-op
	if len(c.Disabled) != 1 {
		t.Errorf("Enable of missing name changed list: %+v", c.Disabled)
	}
}

func TestLoadMalformedErrors(t *testing.T) {
	p := setPath(t)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("expected error for malformed config")
	}
}

func TestPathRespectsXDG(t *testing.T) {
	t.Setenv("RESUMER_CONFIG", "")
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	want := filepath.Join(xdg, "resumer", "config.json")
	if got := Path(); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}
