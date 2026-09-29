package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProviderToggleUI drives the interactive checkbox screen (`resumer
// provider`) through a real PTY: cursor down to opencode, space, enter —
// then verifies the persisted config and the plain-table state.
func TestProviderToggleUI(t *testing.T) {
	materializeFixtureCwds(t)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	env := append(fixtureEnv(t), "RESUMER_CONFIG="+cfgPath)

	// Bare `resumer provider` on a TTY opens the checkbox screen.
	r := startPicker(t, env, "provider")
	r.waitFor(t, "resumer providers", 5*time.Second)
	r.waitFor(t, "[*] opencode", 5*time.Second)

	// claude-code → codex → kimi-code → opencode: three downs, toggle, save.
	r.send("\x1b[B\x1b[B\x1b[B")
	r.send(" ")
	r.waitFor(t, "[ ] opencode", 5*time.Second)
	r.send("\r")
	r.waitExit(t, 5*time.Second)

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if !strings.Contains(string(data), "opencode") {
		t.Errorf("config missing opencode after save: %s", data)
	}

	// The text table (non-TTY) reflects the toggled state.
	out, code := runBin(t, env, "provider", "list")
	if code != 0 || !strings.Contains(out, "opencode") || !strings.Contains(out, "off") {
		t.Errorf("provider list after UI save: exit=%d out=%q", code, out)
	}
	// And ambient scans skip it.
	out, _ = runBin(t, env, "list", "--json", "--all")
	if strings.Contains(out, `"opencode"`) {
		t.Errorf("disabled opencode still scanned:\n%s", out)
	}

	// Re-open and cancel: config must stay untouched.
	before, _ := os.ReadFile(cfgPath)
	r2 := startPicker(t, env, "provider")
	r2.waitFor(t, "[ ] opencode", 5*time.Second) // persisted state rendered
	r2.send("\x1b")                              // esc
	r2.waitExit(t, 5*time.Second)
	after, _ := os.ReadFile(cfgPath)
	if string(before) != string(after) {
		t.Errorf("cancel changed the config:\nbefore=%s\nafter=%s", before, after)
	}
}
