// Package cli is the entry point: flag parsing, dispatch, exit codes, and
// the resume exec flow. Ported from the Python cli.py; the fzf preview hook
// is gone (the native TUI needs no self-reinvocation).
package cli

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/QuietSugar/resumer/internal/config"
	"github.com/QuietSugar/resumer/internal/cwd"
	"github.com/QuietSugar/resumer/internal/execres"
	"github.com/QuietSugar/resumer/internal/provider"
	"github.com/QuietSugar/resumer/internal/provider/codebuddy"
	"github.com/QuietSugar/resumer/internal/provider/kimi"
	"github.com/QuietSugar/resumer/internal/provider/opencode"
	"github.com/QuietSugar/resumer/internal/render"
	"github.com/QuietSugar/resumer/internal/session"
	"github.com/QuietSugar/resumer/internal/tui"
)

func registerProviders() {
	if len(provider.All()) == 0 {
		provider.Register(codebuddy.New())
		provider.Register(kimi.New())
		provider.Register(opencode.New())
	}
}

const usageText = `usage: resumer [list] [options]
       resumer provider [list | on NAME | off NAME]

Unified AI CLI session resumer.

  resumer              interactive picker across all active providers
  resumer list         render merged session list (no interaction)
  resumer provider     interactive on/off checkboxes (space toggle, enter
                       saves); a disabled provider is not scanned or parsed
                       at all until turned back on

options:
  --source NAME    limit to a single provider (codebuddy | kimi-code | opencode)
  --days N         only show sessions active in the last N days (default: no limit)
  --date DATE      YYYY-MM-DD — only sessions active on this date
  --all            no time filter
  --project STR    substring match against project name
  --limit N        top N after sort
  --no-group       list mode: flat table, do not group by workspace
  --json           list mode only: emit JSON array of sessions
  --full [N]       list mode only: render detailed boxes for top N (default 5)
  --version        print version

Disabled providers are recorded in ~/.config/resumer/config.json (override
with $RESUMER_CONFIG) and are skipped by ambient scans. An explicit
--source NAME still works on a disabled provider: the flag is a deliberate
per-invocation request.
`

// valueFlags take a separate argument (so "list" after them is a value, not
// the subcommand).
var valueFlags = map[string]bool{
	"--source": true, "--days": true, "--date": true,
	"--project": true, "--limit": true,
}

// normalize extracts the optional subcommand ("list" or "provider") and
// rewrites the argparse-style "--full [N]" into flag-friendly "--full=N".
func normalize(argv []string) (command string, out []string) {
	expectValue := false
	for i := 0; i < len(argv); i++ {
		tok := argv[i]
		switch {
		case expectValue:
			expectValue = false
			out = append(out, tok)
		case command == "" && (tok == "list" || tok == "provider"):
			command = tok
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

	// The provider subcommand has its own mini-parsing (positional args, no
	// flags) and its own config-error policy: mutating commands must not
	// silently proceed on an unreadable config, or Save would clobber it.
	if command == "provider" {
		return runProviderCmd(args)
	}

	// Every other path only reads the config: degrade to defaults with a
	// warning instead of failing the whole invocation.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: ignoring unreadable config: %v\n", err)
	}
	provider.SetDisabled(cfg.Disabled)

	fs := flag.NewFlagSet("resumer", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usageText) }
	var (
		showVersion = fs.Bool("version", false, "print version")
		source      = fs.String("source", "", "limit to a single provider")
		days        = fs.Int("days", 0, "only show sessions active in the last N days; 0 = no limit")
		date        = fs.String("date", "", "YYYY-MM-DD")
		all         = fs.Bool("all", false, "no time filter")
		project     = fs.String("project", "", "project substring")
		limit       = fs.Int("limit", 0, "top N after sort")
		noGroup     = fs.Bool("no-group", false, "flat table (list mode)")
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
	validSources := map[string]bool{"codebuddy": true, "kimi-code": true, "opencode": true}
	if *source != "" && !validSources[*source] {
		fmt.Fprintf(os.Stderr,
			"error: argument --source: invalid choice: %q (choose from codebuddy, kimi-code, opencode)\n", *source)
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
	// diagnostics. A disabled provider counts as "handled": point the user
	// at `provider on` rather than claiming nothing is installed.
	if *source == "" && len(provider.AvailableSourceNames()) == 0 {
		msg := "error: no session providers available. " +
			"Install CodeBuddy, Kimi, or OpenCode and ensure their session storage exists."
		if dis := provider.DisabledNames(); len(dis) > 0 {
			msg = fmt.Sprintf(
				"error: no enabled session providers available (disabled: %s). "+
					"Run `resumer provider on <name>` to re-enable one.",
				strings.Join(dis, ", "))
		}
		fmt.Fprintln(os.Stderr, msg)
		return 2
	}

	// --no-group is a display hint only; harmless outside list mode, so it is
	// ignored rather than rejected.
	if command != "list" && (*jsonOut || *full >= 0) {
		fmt.Fprintln(os.Stderr,
			"error: --json and --full are only valid with the 'list' subcommand")
		return 2
	}

	if command == "list" {
		return runList(filters, *jsonOut, *full, *noGroup)
	}
	return runPicker(filters)
}

func runList(filters session.Filters, jsonOut bool, full int, noGroup bool) int {
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
	case noGroup:
		fmt.Println(render.Index(sessions))
	default:
		fmt.Println(render.IndexGrouped(sessions))
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
			fmt.Fprintln(os.Stderr, "No sessions found. Try without --project/--date, or check `resumer provider list`.")
		}
		return 0
	}
	return execResume(chosen)
}

// runProviderCmd implements `resumer provider [list | on NAME | off NAME]`.
// State is persisted to the config file; unknown subcommands exit 2.
func runProviderCmd(args []string) int {
	sub := "" // bare `resumer provider` — the interactive toggle screen
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}

	switch sub {
	case "":
		// Bare `resumer provider` — interactive checkbox screen on a TTY,
		// the plain table otherwise.
		return runProviderToggleUI()

	case "list":
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 2
		}
		return printProviderList(cfg)

	case "on", "off":
		if len(args) == 0 {
			fmt.Fprintf(os.Stderr, "error: provider %s requires a name (choose from %s)\n",
				sub, strings.Join(providerNames(), ", "))
			return 2
		}
		name := args[0]
		if provider.Get(name) == nil {
			fmt.Fprintf(os.Stderr, "error: unknown provider: %q (choose from %s)\n",
				name, strings.Join(providerNames(), ", "))
			return 2
		}
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: refusing to rewrite unreadable config: %v\n", err)
			return 2
		}
		if sub == "off" {
			cfg.Disable(name)
			if err := config.Save(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "error: saving config: %v\n", err)
				return 2
			}
			fmt.Printf("%s disabled — resumer will no longer scan or parse it.\n", name)
		} else {
			cfg.Enable(name)
			if err := config.Save(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "error: saving config: %v\n", err)
				return 2
			}
			fmt.Printf("%s enabled.\n", name)
		}
		return 0

	default:
		fmt.Fprintf(os.Stderr,
			"error: unknown provider subcommand: %q (choose from list, on, off)\n", sub)
		return 2
	}
}

// printProviderList writes the plain text state table (also the fallback for
// `resumer provider` when stdin is not a TTY).
func printProviderList(cfg config.Config) int {
	fmt.Printf("%-12s %-5s %-10s %s\n", "PROVIDER", "STATE", "STORAGE", "CONFIG")
	for _, p := range provider.All() {
		state, storage := "on", "ok"
		cfgNote := "-"
		if !p.IsAvailable() {
			storage = "missing"
		}
		if cfg.IsDisabled(p.Name()) {
			state = "off"
			cfgNote = "disabled"
		}
		fmt.Printf("%-12s %-5s %-10s %s\n", p.Name(), state, storage, cfgNote)
	}
	return 0
}

// runProviderToggleUI opens the interactive checkbox screen for bare
// `resumer provider`. On save it persists the resulting disabled list; esc
// leaves the config untouched. Scripted (non-TTY) stdin falls back to the
// text table instead of hanging on a UI it cannot answer.
func runProviderToggleUI() int {
	// Mutating path: never proceed on an unreadable config — Save would
	// clobber it.
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: refusing to rewrite unreadable config: %v\n", err)
		return 2
	}
	if !stdinIsTerminal() {
		return printProviderList(cfg)
	}

	disabled := map[string]bool{}
	for _, d := range cfg.Disabled {
		disabled[d] = true
	}
	var names []string
	storage := map[string]bool{}
	for _, p := range provider.All() {
		names = append(names, p.Name())
		storage[p.Name()] = p.IsAvailable()
	}

	result, canceled, err := tui.ProviderToggle(names, disabled, storage)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 2
	}
	if canceled {
		fmt.Println("cancelled — config unchanged.")
		return 0
	}
	sameSet := len(cfg.Disabled) == len(result)
	if sameSet {
		want := map[string]bool{}
		for _, d := range cfg.Disabled {
			want[d] = true
		}
		for _, r := range result {
			if !want[r] {
				sameSet = false
				break
			}
		}
	}
	if sameSet {
		fmt.Println("no changes.")
		return 0
	}
	cfg.Disabled = result
	sort.Strings(cfg.Disabled)
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "error: saving config: %v\n", err)
		return 2
	}
	if len(result) == 0 {
		fmt.Println("all providers enabled.")
	} else {
		fmt.Printf("disabled: %s\n", strings.Join(result, ", "))
	}
	return 0
}

// stdinIsTerminal reports whether stdin is an interactive terminal. Uses the
// charmbracelet/x/term package already in the dependency graph (bubbletea's
// own terminal layer) — no new module.
func stdinIsTerminal() bool {
	return term.IsTerminal(os.Stdin.Fd())
}

// providerNames lists registered provider names in registry order.
func providerNames() []string {
	var out []string
	for _, p := range provider.All() {
		out = append(out, p.Name())
	}
	return out
}

// exitCannotResume is returned when a session's working directory is gone, so
// the agent CLI would fail on its own with a "created under a different
// directory" error that names neither the real cause nor the fix.
const exitCannotResume = 3

// reportMissingCwd explains the refusal and hands the user the one command
// that unblocks it, ready to paste.
func reportMissingCwd(s *session.Session, dir string) {
	fmt.Fprintf(os.Stderr,
		"error: cannot resume [%s] %s — its working directory has been deleted:\n",
		s.Source, s.SessionID)
	fmt.Fprintf(os.Stderr, "       %s\n", dir)
	fmt.Fprintf(os.Stderr,
		"       the agent CLI cannot start there, so resumer did not run the resume command.\n")
	fmt.Fprintf(os.Stderr, "       to resume this session, recreate the directory first:\n")
	fmt.Fprintf(os.Stderr, "         mkdir -p %q\n", dir)
	fmt.Fprintf(os.Stderr, "       then run resumer again and select this session.\n")
}

// execResume chdirs into the session's directory and replaces the process
// with the provider's resume command.
//
// When the resolved directory does not exist at all, resumer refuses instead
// of exec'ing: the agent CLI would only report that the session "was created
// under a different directory", which tells the user nothing about the real
// problem or how to fix it.
func execResume(s *session.Session) int {
	targetCwd, ok := cwd.Resolve(s)
	if !ok {
		reportMissingCwd(s, targetCwd)
		return exitCannotResume
	}
	if targetCwd != "" {
		_ = os.Chdir(targetCwd)
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
			"codebuddy": "https://www.codebuddy.ai/docs/cli/reference",
			"kimi":      "https://github.com/MoonshotAI/kimi-code",
			"opencode":  "https://opencode.ai/docs",
		}[binName]
		fmt.Fprintf(os.Stderr, "error: '%s' not found in PATH\n", binName)
		if installHint != "" {
			fmt.Fprintf(os.Stderr, "       install: %s\n", installHint)
		}
		return 127
	}
	return execres.Exec(binPath, s.ResumeArgv)
}
