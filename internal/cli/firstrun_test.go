package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	fn()
	w.Close()
	os.Stderr = orig
	out, _ := io.ReadAll(r)
	return string(out)
}

func fakeBinDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "codebuddy")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStateDirHonorsXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	if got := stateDir(); got != filepath.Join(tmp, "resumer") {
		t.Errorf("stateDir = %q", got)
	}
}

func TestMissingBinSkipsMessageAndSentinel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	t.Setenv("PATH", t.TempDir()) // empty dir — no codebuddy on PATH
	out := captureStderr(t, func() { maybeShowFirstRunStar("codebuddy") })
	if out != "" {
		t.Errorf("no message expected when bin missing, got %q", out)
	}
	sentinel := filepath.Join(tmp, "resumer", "first-run-done")
	if _, err := os.Stat(sentinel); err == nil {
		t.Error("sentinel must not be created when bin missing")
	}
}

func TestPresentBinPrintsAndCreatesSentinel(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	t.Setenv("PATH", fakeBinDir(t))
	out := captureStderr(t, func() { maybeShowFirstRunStar("codebuddy") })
	if !strings.Contains(out, "⭐") || !strings.Contains(out, "github.com/QuietSugar/resumer") {
		t.Errorf("star message missing: %q", out)
	}
	sentinel := filepath.Join(tmp, "resumer", "first-run-done")
	if _, err := os.Stat(sentinel); err != nil {
		t.Error("sentinel must be created on first successful gate")
	}
}

func TestSentinelPresentIsSilent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)
	t.Setenv("PATH", fakeBinDir(t))
	sentinel := filepath.Join(tmp, "resumer", "first-run-done")
	if err := os.MkdirAll(filepath.Dir(sentinel), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	out := captureStderr(t, func() { maybeShowFirstRunStar("codebuddy") })
	if out != "" {
		t.Errorf("message must not repeat when sentinel exists, got %q", out)
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct {
		in      []string
		wantCmd string
		wantOut string
	}{
		{[]string{"list", "--json"}, "list", "--json"},
		{[]string{"--days", "3", "list"}, "list", "--days 3"},
		{[]string{"--full"}, "", "--full=5"},
		{[]string{"--full", "2"}, "", "--full=2"},
		{[]string{"list", "--full", "3"}, "list", "--full=3"},
		{[]string{"--project", "list"}, "", "--project list"}, // "list" as a flag value
	}
	for _, c := range cases {
		cmd, out := normalize(c.in)
		if cmd != c.wantCmd || strings.Join(out, " ") != c.wantOut {
			t.Errorf("normalize(%v) = (%q, %q), want (%q, %q)",
				c.in, cmd, strings.Join(out, " "), c.wantCmd, c.wantOut)
		}
	}
}
