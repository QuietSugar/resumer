// Package cli is the entry point: flag parsing, dispatch, exit codes, and
// the resume exec flow. Ported from the Python cli.py; the fzf preview hook
// is gone (the native TUI needs no self-reinvocation).
package cli

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/jin-ttao/resumer/internal/execres"
	"github.com/jin-ttao/resumer/internal/provider"
	"github.com/jin-ttao/resumer/internal/provider/claudecode"
	"github.com/jin-ttao/resumer/internal/provider/codex"
	"github.com/jin-ttao/resumer/internal/provider/kimi"
	"github.com/jin-ttao/resumer/internal/provider/opencode"
	"github.com/jin-ttao/resumer/internal/render"
	"github.com/jin-ttao/resumer/internal/session"
	"github.com/jin-ttao/resumer/internal/tui"
)

func registerProviders() {
	if len(provider.All()) == 0 {
		provider.Register(claudecode.New())
		provider.Register(codex.New())
		provider.Register(kimi.New())
		provider.Register(opencode.New())
	}
}

const usageText = `usage: resumer [list] [options]

Unified AI CLI session resumer.

  resumer              interactive picker across all active providers
  resumer list         render merged session list (no interaction)

options:
  --source NAME    limit to a single provider (claude-code | codex | kimi-code | opencode)
  --days N         only show sessions active in the last N days (default: 7)
  --date DATE      YYYY-MM-DD — only sessions active on this date
  --all            no time filter
  --project STR    substring match against project name
  --limit N        top N after sort
  --json           list mode only: emit JSON array of sessions
  --full [N]       list mode only: render detailed boxes for top N (default 5)
  --version        print version
`

// valueFlags take a separate argument (so "list" after them is a value, not
// the subcommand).
var valueFlags = map[string]bool{
	"--source": true, "--days": true, "--date": true,
	"--project": true, "--limit": true,
}

// normalize extracts the optional "list" subcommand and rewrites the
// argparse-style "--full [N]" into flag-friendly "--full=N".
func normalize(argv []string) (command string, out []string) {
	expectValue := false
	for i := 0; i < len(argv); i++ {
		tok := argv[i]
		switch {
		case expectValue:
			expectValue = false
			out = append(out, tok)
		case tok == "list" && command == "":
			command = "list"
		case tok == "--full":
			n := 5
			if i+1 < len(argv) {
				if v, err := strconv.Atoi(argv[i+1]); err == nil {
					n = v
					i++
				}
			}
			out = append(out, fmt.Sprintf("--full=%d", n))
		case valueFlags[tok]:
			expectValue = true
			out = append(out, tok)
		default:
			out = append(out, tok)
		}
	}
	return command, out
}

// Run executes the CLI and returns the process exit code.
func Run(argv []string, version string) int {
	registerProviders()

	command, args := normalize(argv)

	fs := flag.NewFlagSet("resumer", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usageText) }
	var (
		showVersion = fs.Bool("version", false, "print version")
		source      = fs.String("source", "", "limit to a single provider")
		days        = fs.Int("days", 7, "window in days")
		date        = fs.String("date", "", "YYYY-MM-DD")
		all         = fs.Bool("all", false, "no time filter")
		project     = fs.String("project", "", "project substring")
		limit       = fs.Int("limit", 0, "top N after sort")
		jsonOut     = fs.Bool("json", false, "JSON output (list mode)")
		full        = fs.Int("full", -1, "detail boxes for top N (list mode)")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "error: unrecognized argument: %s\n", fs.Arg(0))
		return 2
	}
	if *showVersion {
		fmt.Printf("resumer %s\n", version)
		return 0
	}
	validSources := map[string]bool{"claude-code": true, "codex": true, "kimi-code": true, "opencode": true}
	if *source != "" && !validSources[*source] {
		fmt.Fprintf(os.Stderr,
			"error: argument --source: invalid choice: %q (choose from claude-code, codex, kimi-code, opencode)\n", *source)
		return 2
	}

	filters := session.Filters{
		Days:    *days,
		Date:    *date,
		AllTime: *all,
		Project: *project,
		Limit:   *limit,
		Source:  *source,
	}
	if *all {
		filters.Days = -1
	}

	// Global availability check only when no specific source requested;
	// otherwise MergedList returns a source-specific error with better
	// diagnostics.
	if *source == "" && len(provider.AvailableSourceNames()) == 0 {
		fmt.Fprintln(os.Stderr,
			"error: no session providers available. "+
				"Install claude-code, codex, kimi, or opencode and ensure their session storage exists.")
		return 2
	}

	if command != "list" && (*jsonOut || *full >= 0) {
		fmt.Fprintln(os.Stderr,
			"error: --json and --full are only valid with the 'list' subcommand")
		return 2
	}

	if command == "list" {
		return runList(filters, *jsonOut, *full)
	}
	return runPicker(filters)
}

func runList(filters session.Filters, jsonOut bool, full int) int {
	sessions, err := provider.MergedList(filters)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}
	switch {
	case jsonOut:
		fmt.Println(render.JSON(sessions))
	case full >= 0:
		n := full
		if n > len(sessions) {
			n = len(sessions)
		}
		for i := 0; i < n; i++ {
			fmt.Println(render.FullBox(&sessions[i]))
			fmt.Println()
		}
	default:
		fmt.Println(render.Index(sessions))
	}
	return 0
}

func runPicker(filters session.Filters) int {
	chosen, empty, err := tui.Pick(filters)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}
	if chosen == nil {
		if empty {
			fmt.Fprintln(os.Stderr, "No sessions found. Try --days 7 or --all.")
		}
		return 0
	}
	return execResume(chosen)
}

// execResume chdirs into the session's directory and replaces the process
// with the provider's resume command.
//
// For claude-code, prefer a cwd derived from the session file's encoded
// parent dir. Stored cwd in the JSONL can be stale/mismatched (seen with
// iCloud/Obsidian paths), causing `claude --resume` to fail because it
// derives the project dir from the current cwd.
func execResume(s *session.Session) int {
	targetCwd := ""
	if s.Source == "claude-code" {
		targetCwd = claudecode.ResolveExecCwd(s.Path, s.Cwd)
	}
	if targetCwd == "" {
		targetCwd = s.Cwd
	}

	if targetCwd != "" {
		if st, err := os.Stat(targetCwd); err == nil && st.IsDir() {
			_ = os.Chdir(targetCwd)
		} else {
			wd, _ := os.Getwd()
			fmt.Fprintf(os.Stderr,
				"warning: session cwd not accessible, running from %s: %s\n", wd, targetCwd)
		}
	}

	if len(s.ResumeArgv) == 0 {
		fmt.Fprintln(os.Stderr, "error: session has no resume command")
		return 2
	}
	maybeShowFirstRunStar(s.ResumeArgv[0])
	wd, _ := os.Getwd()
	fmt.Fprintf(os.Stderr, "resuming [%s] %s from %s\n", s.Source, s.SessionID, wd)

	binPath, err := exec.LookPath(s.ResumeArgv[0])
	if err != nil {
		binName := s.ResumeArgv[0]
		installHint := map[string]string{
			"claude":   "https://docs.anthropic.com/en/docs/claude-code/quickstart",
			"codex":    "https://github.com/openai/codex",
			"kimi":     "https://github.com/MoonshotAI/kimi-code",
			"opencode": "https://opencode.ai/docs",
		}[binName]
		fmt.Fprintf(os.Stderr, "error: '%s' not found in PATH\n", binName)
		if installHint != "" {
			fmt.Fprintf(os.Stderr, "       install: %s\n", installHint)
		}
		return 127
	}
	return execres.Exec(binPath, s.ResumeArgv)
}
