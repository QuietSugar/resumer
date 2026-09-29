// Package integration runs the built resumer binary against the fixture set,
// porting the old shell assertion scenarios (08 unified render, 10 missing
// provider, 12 stale-cwd exec). The TUI scenarios drive a real PTY, so the
// full path — picker → filter → enter → chdir → exec of the mock claude —
// is proven without tmux.
package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

var (
	repoRoot string
	binPath  string
)

func TestMain(m *testing.M) {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot, _ = filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", ".."))

	tmp, err := os.MkdirTemp("", "resumer-itest-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmp)
	binPath = filepath.Join(tmp, "resumer")

	build := exec.Command("go", "build", "-o", binPath, ".")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n%s", err, out)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func fixtureEnv(t *testing.T) []string {
	t.Helper()
	mockBin := filepath.Join(repoRoot, "tests", "mock-bin")
	env := []string{
		"PATH=" + mockBin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"TERM=xterm-256color",
		"RESUMER_CLAUDE_PROJECT_ROOT=" + filepath.Join(repoRoot, "tests", "fixtures", "claude-code"),
		"RESUMER_CODEX_SESSION_ROOT=" + filepath.Join(repoRoot, "tests", "fixtures", "codex"),
		"RESUMER_CODEX_INDEX_FILE=" + filepath.Join(repoRoot, "tests", "fixtures", "codex", "session_index.jsonl"),
		"RESUMER_CODEX_BIN=codex",
		"RESUMER_KIMI_HOME=" + filepath.Join(repoRoot, "tests", "fixtures", "kimi-home"),
		"RESUMER_KIMI_BIN=kimi",
		"RESUMER_OPENCODE_DATA=" + filepath.Join(repoRoot, "tests", "fixtures", "opencode-home"),
		"RESUMER_OPENCODE_BIN=opencode",
		// Sentinel pre-burned via XDG redirect so the first-run star message
		// doesn't interleave with exec assertions.
		"XDG_STATE_HOME=" + t.TempDir(),
	}
	return env
}

// materializeFixtureCwds creates the real directories fixture sessions point
// at (the old tmux_use_fixtures did this), so chdir before exec succeeds.
func materializeFixtureCwds(t *testing.T) {
	t.Helper()
	for _, d := range []string{
		"/tmp/resumer-fixtures/alpha",
		"/tmp/resumer-fixtures/beta",
		"/tmp/resumer-fixtures/obsidian path with space/vault",
		"/tmp/resumer-fixtures/kimi-one",
		"/tmp/resumer-fixtures/kimi-two",
		"/tmp/resumer-fixtures/oc-one",
		"/tmp/resumer-fixtures/oc-two",
		"/tmp/resumer-fixtures/oc-three",
		"/tmp/resumer-fixtures/oc-json",
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// --- 08: unified render ---

func TestUnifiedRender(t *testing.T) {
	cmd := exec.Command(binPath, "list", "--all")
	cmd.Env = fixtureEnv(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("list --all failed: %v", err)
	}
	s := string(out)
	for _, want := range []string{"[cc]", "[codex]", " alpha ", " beta "} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q", want)
		}
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("unexpectedly short output: %d lines", len(lines))
	}
	// Header(1) + divider(2); first data row must be the most recent fixture:
	// codex-three at 07:00 beats every cc row (≤ 04:30).
	firstRow := lines[2]
	if !strings.Contains(firstRow, "codex-three") || !strings.Contains(firstRow, "[codex]") {
		t.Errorf("top row should be codex-three with [codex] badge: %q", firstRow)
	}
	lastRow := lines[len(lines)-1]
	if !strings.Contains(lastRow, " alpha ") || !strings.Contains(lastRow, "plain fixture") {
		t.Errorf("last row should be oldest cc alpha session: %q", lastRow)
	}
}

// --- 10: missing provider ---

func TestMissingProvider(t *testing.T) {
	env := fixtureEnv(t)
	for i, e := range env {
		if strings.HasPrefix(e, "RESUMER_CODEX_SESSION_ROOT=") {
			env[i] = "RESUMER_CODEX_SESSION_ROOT=/nonexistent/resumer-qa-missing"
		}
	}

	cmd := exec.Command(binPath, "list", "--all")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("merged list must exit 0 with claude-only providers: %v", err)
	}
	if !strings.Contains(string(out), "[cc]") {
		t.Error("output should have [cc] rows")
	}
	if strings.Contains(string(out), "[codex]") {
		t.Error("output must have no [codex] rows")
	}

	cmd2 := exec.Command(binPath, "list", "--source=codex", "--all")
	cmd2.Env = env
	var stderr strings.Builder
	cmd2.Stderr = &stderr
	err2 := cmd2.Run()
	ee, ok := err2.(*exec.ExitError)
	if !ok || ee.ExitCode() != 2 {
		t.Fatalf("--source=codex must exit 2 when unavailable, got %v", err2)
	}
	if !strings.Contains(stderr.String(), "codex provider not available") {
		t.Errorf("stderr should carry provider-specific message: %q", stderr.String())
	}
}

// --- PTY harness for picker scenarios ---

type ptyRun struct {
	cmd  *exec.Cmd
	tty  *os.File
	mu   sync.Mutex
	buf  strings.Builder
	done chan error
}

func startPicker(t *testing.T, env []string, args ...string) *ptyRun {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	cmd.Env = env
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 55, Cols: 220})
	if err != nil {
		t.Fatal(err)
	}
	r := &ptyRun{cmd: cmd, tty: tty, done: make(chan error, 1)}
	go func() {
		// Act as a minimal terminal emulator: termenv/lipgloss probe the
		// terminal (OSC 11 background color, CSI 6n cursor position) and
		// block rendering until a reply arrives. Real terminals and tmux
		// answer instantly; this harness must too.
		b := make([]byte, 4096)
		for {
			n, err := tty.Read(b)
			if n > 0 {
				chunk := string(b[:n])
				r.mu.Lock()
				r.buf.Write(b[:n])
				r.mu.Unlock()
				if strings.Contains(chunk, "\x1b]11;?") {
					tty.WriteString("\x1b]11;rgb:0000/0000/0000\x07")
				}
				if strings.Contains(chunk, "\x1b[6n") {
					tty.WriteString("\x1b[1;1R")
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { r.done <- cmd.Wait() }()
	t.Cleanup(func() {
		tty.Close()
		cmd.Process.Kill()
	})
	return r
}

func (r *ptyRun) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func (r *ptyRun) waitFor(t *testing.T, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(r.output(), substr) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	dump := filepath.Join(os.TempDir(), "resumer-itest-dump.txt")
	_ = os.WriteFile(dump, []byte(r.output()), 0o644)
	t.Fatalf("timeout waiting for %q in picker output; full buffer dumped to %s; last screen:\n%s",
		substr, dump, tail(r.output(), 2000))
}

func (r *ptyRun) send(s string) {
	r.tty.WriteString(s)
}

func (r *ptyRun) waitExit(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-r.done:
	case <-time.After(timeout):
		t.Fatal("picker process did not exit in time")
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func waitForFile(t *testing.T, path string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return string(b)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("file %s never appeared", path)
	return ""
}

// --- 12: stale-cwd exec through the real TUI ---

func TestStaleCwdExec(t *testing.T) {
	materializeFixtureCwds(t)
	logPath := filepath.Join(t.TempDir(), "claude-mock.log")
	env := append(fixtureEnv(t), "CLAUDE_MOCK_LOG="+logPath)

	r := startPicker(t, env, "--source=claude-code", "--all")
	r.waitFor(t, "[cc]", 5*time.Second)

	// Filter down to the stale-cwd fixture, apply, select.
	r.send("/stale cwd regression")
	time.Sleep(400 * time.Millisecond)
	r.send("\r") // apply filter
	time.Sleep(300 * time.Millisecond)
	r.send("\r") // resume selection
	r.waitExit(t, 5*time.Second)

	log := waitForFile(t, logPath, 5*time.Second)
	t.Logf("mock claude log: %s", strings.TrimSpace(log))

	pwdRE := regexp.MustCompile(`pwd=(/private)?/tmp/resumer-fixtures/obsidian path with space/vault`)
	if !pwdRE.MatchString(log) {
		t.Errorf("expected walk to real vault path, log: %q", log)
	}
	if !strings.Contains(log, "args=--resume dddddddd-0006-4000-8000-000000000006") {
		t.Errorf("expected claude --resume with correct uuid, log: %q", log)
	}
	if strings.Contains(log, "pwd=/bogus/wrong/path") {
		t.Error("pwd ended at stored bogus cwd — stale-cwd fix not applied")
	}
}

// --- picker cancel (port of the 07/09 interaction essentials) ---

func TestPickerCancelLeavesNoLog(t *testing.T) {
	materializeFixtureCwds(t)
	logPath := filepath.Join(t.TempDir(), "claude-mock.log")
	env := append(fixtureEnv(t), "CLAUDE_MOCK_LOG="+logPath)

	r := startPicker(t, env, "--all")
	r.waitFor(t, "[cc]", 5*time.Second)
	r.send("\x1b") // esc → cancel
	r.waitExit(t, 5*time.Second)

	if _, err := os.Stat(logPath); err == nil {
		t.Error("cancel must not exec the resume binary")
	}
}

// --- sort toggle + source cycle interactions ---

func TestSortToggleAndSourceCycle(t *testing.T) {
	materializeFixtureCwds(t)
	logPath := filepath.Join(t.TempDir(), "claude-mock.log")
	env := append(fixtureEnv(t), "CLAUDE_MOCK_LOG="+logPath)

	r := startPicker(t, env, "--all")
	r.waitFor(t, "[codex]", 5*time.Second)

	// ctrl-s twice (asc → desc again), tab through sources and back to all.
	r.send("\x13") // ctrl+s
	time.Sleep(200 * time.Millisecond)
	r.send("\x13")
	time.Sleep(200 * time.Millisecond)
	r.send("\t") // → claude-code only
	time.Sleep(200 * time.Millisecond)
	r.waitFor(t, "source: claude-code", 3*time.Second)
	r.send("\t") // → codex only
	time.Sleep(200 * time.Millisecond)
	r.waitFor(t, "source: codex", 3*time.Second)
	r.send("\t") // → back to all
	time.Sleep(300 * time.Millisecond)

	// Toggling must leave the picker functional and must not exec anything;
	// exec behavior itself is covered by the dedicated tests above.
	r.send("\x1b") // cancel
	r.waitExit(t, 5*time.Second)

	if _, err := os.Stat(logPath); err == nil {
		t.Error("sort/source toggling must not trigger an exec")
	}
}

// --- codex selection end-to-end ---

func TestCodexSelectExec(t *testing.T) {
	materializeFixtureCwds(t)
	logPath := filepath.Join(t.TempDir(), "codex-mock.log")
	env := append(fixtureEnv(t), "CODEX_MOCK_LOG="+logPath)

	r := startPicker(t, env, "--source=codex", "--all")
	r.waitFor(t, "[codex]", 5*time.Second)
	// Top row is the most recent codex session (codex-three). Select it.
	r.send("\r")
	r.waitExit(t, 5*time.Second)

	log := waitForFile(t, logPath, 5*time.Second)
	if !strings.Contains(log, "args=resume 019cccc3-3333-7000-8000-000000000003") {
		t.Errorf("expected codex resume of most-recent session, log: %q", log)
	}
}

// --- kimi-code selection end-to-end ---

func TestKimiSelectExec(t *testing.T) {
	materializeFixtureCwds(t)
	logPath := filepath.Join(t.TempDir(), "kimi-mock.log")
	env := append(fixtureEnv(t), "KIMI_MOCK_LOG="+logPath)

	r := startPicker(t, env, "--source=kimi-code", "--all")
	r.waitFor(t, "[kimi]", 5*time.Second)
	// Top row is the most recent kimi session (kimi-two, 06:32). Select it;
	// exec must chdir into the workDir recorded in session_index.jsonl.
	r.send("\r")
	r.waitExit(t, 5*time.Second)

	log := waitForFile(t, logPath, 5*time.Second)
	if !strings.Contains(log, "args=--session dddd0002-2222-7000-8000-000000000002") {
		t.Errorf("expected kimi --session of most-recent session, log: %q", log)
	}
	pwdRE := regexp.MustCompile(`pwd=(/private)?/tmp/resumer-fixtures/kimi-two`)
	if !pwdRE.MatchString(log) {
		t.Errorf("expected exec from the session workDir, log: %q", log)
	}
}

// --- opencode selection end-to-end ---

func TestOpenCodeSelectExec(t *testing.T) {
	materializeFixtureCwds(t)
	logPath := filepath.Join(t.TempDir(), "opencode-mock.log")
	env := append(fixtureEnv(t), "OPENCODE_MOCK_LOG="+logPath)

	r := startPicker(t, env, "--source=opencode", "--all")
	r.waitFor(t, "[oc]", 5*time.Second)
	// Top row is ses_ddd (06:21, oc-three) — newest visible opencode session
	// (archived and child fixtures are filtered out).
	r.send("\r")
	r.waitExit(t, 5*time.Second)

	log := waitForFile(t, logPath, 5*time.Second)
	if !strings.Contains(log, "args=--session ses_dddddddddddddddddddddddddddd") {
		t.Errorf("expected opencode --session of most-recent session, log: %q", log)
	}
	pwdRE := regexp.MustCompile(`pwd=(/private)?/tmp/resumer-fixtures/oc-three`)
	if !pwdRE.MatchString(log) {
		t.Errorf("expected exec from the session directory, log: %q", log)
	}
}
