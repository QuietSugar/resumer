package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const starURL = "https://github.com/QuietSugar/resumer"

func stateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "resumer")
}

// maybeShowFirstRunStar prints a one-time star nudge on the user's first
// successful resume.
//
// Guard: if the resume binary itself is not on PATH, skip both the message
// and the sentinel. That way the message fires only when the user is actually
// about to experience the tool — not on a failed first attempt that would
// otherwise burn the sentinel.
func maybeShowFirstRunStar(resumeBin string) {
	if _, err := exec.LookPath(resumeBin); err != nil {
		return
	}
	sentinel := filepath.Join(stateDir(), "first-run-done")
	if _, err := os.Stat(sentinel); err == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(sentinel), 0o755); err != nil {
		return // state dir not writable — message can wait
	}
	f, err := os.Create(sentinel)
	if err != nil {
		return
	}
	f.Close()
	fmt.Fprintf(os.Stderr, "\n  Enjoyed resumer? ⭐ %s\n\n", starURL)
}
