# resumer

> Browse & resume CodeBuddy, Kimi Code, and OpenCode sessions — one picker, zero dependencies.

![resumer demo](docs/demo.gif)

A single static binary. No Python, no fzf, no setup. Open the picker, see every
recent AI-CLI session across providers with a full preview, hit Enter, and you're
back in the conversation.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/QuietSugar/resumer/main/install.sh | sh
```

or with Go:

```bash
go install github.com/QuietSugar/resumer@latest
```

macOS & Linux (arm64/amd64). Windows is on the roadmap.

## Usage

```bash
resumer              # interactive picker
resumer list         # grouped by workspace, every session
resumer --help       # everything else
```

Picker keys: `↑↓` browse · `/` filter · `tab` cycle source · `ctrl-s` toggle sort ·
`enter` resume · `esc` cancel. They are listed on the last line of the
screen. The session list carries a one-line column header (`age` `agent`
`title`) so the fixed-width rows are self-describing, and sessions are grouped
under a workspace row shaped like `2 sessions · …/h/d/g/g/Q/resumer` — session
count first, then the abbreviated directory (leading components collapsed to
their first letters, the final directory always in full). The trailing marker
column is a rough conversation-weight bar, colored gray → yellow → red in the
picker: blank <20 messages, `▁` 20–49, `▄` 50–149, `█` 150+.

Useful flags (both picker and `list`): `--days N` (narrow to the last N days —
**the default is no time limit, so every session is listed**), `--date YYYY-MM-DD`,
`--all` (kept for compatibility; it is now the default), `--project foo`,
`--source codebuddy|kimi-code|opencode`, `--limit N`. List mode adds
`--no-group` (flat table instead of workspace groups), `--json`, and `--full [N]`.
The `asst_count` JSON field and detail-preview
assistant activity figure are provider-specific rough size estimates; counting
rules differ and the values are not exact or directly comparable across providers.

The picker tips pane uses provider-specific built-in tips by default. To replace
these with your own plain-text tips, create `~/.config/resumer/tips.md` or set
`RESUMER_TIPS_FILE` to another file path. The file is read when the picker
starts; a missing or blank file falls back to the built-in tips.

## Workspaces

`resumer list` and the picker group sessions by **workspace**: sessions opened in
the same working directory are grouped together, across providers. A workspace
header looks like:

```
── /home/xu/git-repo/my-project  ·  3 sessions  ·  codebuddy, kimi-code
```

The grouping key is the session's working directory, so a directory used from
several agents lands in one group. When a provider records a native
workspace/project identifier (Kimi Code's `wd_<slug>_<hash>` session bucket and
`workspaces.json`; OpenCode's `workspace_id`/`project_id`), it is used as the
group's identity — and as the last-resort key when no directory is known.
Sessions with neither a directory nor a native id are shown under
`(no workspace)`, scoped per provider.

`--json` stays a flat array (add the `workspace_id`/`workspace_root` fields
yourself if you need to group downstream). Use `resumer list --no-group` for the
old flat table, or `--source NAME` to restrict to a single provider (no
cross-provider merging then).

## Enabling / disabling providers

Each provider can be turned off; a disabled provider is **not scanned or
parsed at all** — no storage walk, no file/db reads — until turned back on.

```bash
resumer provider             # interactive checkboxes — ↑↓ move, space
                             # toggle, enter saves, esc cancels
resumer provider list        # plain table: state + storage detected?
resumer provider off opencode
resumer provider on opencode
```

`resumer provider` needs a terminal; piped stdin prints the table instead.
Untoggling everything and saving re-enables all providers; providers that
have disappeared from resumer (config outlived an upgrade) show as
`(unknown)` so stale entries stay visible and clearable.

The state persists in `~/.config/resumer/config.json` (`$RESUMER_CONFIG`
overrides the path; `$XDG_CONFIG_HOME` overrides its directory). Disabled
providers disappear from ambient scans and the picker's `tab` source cycle.
An explicit `resumer list --source opencode` still works on a disabled
provider — the flag is a deliberate per-invocation request.

## Providers

| Provider | Session source | Resume command |
|---|---|---|
| CodeBuddy CLI | `~/.codebuddy/projects/**/*.jsonl` | `codebuddy --resume <id>` |
| Kimi Code CLI | `~/.kimi-code/sessions/<workDirKey>/<id>/` | `kimi --session <id>` |
| OpenCode | `~/.local/share/opencode/opencode.db` (≤1.0: `storage/session/`) | `opencode --session <id>` |

For multi-agent format research, see [TokenTracker](https://github.com/xiufengsun/TokenTracker):
the inspected snapshot's README lists 42 supported AI tools, including CodeBuddy, and its
CodeBuddy notes describe `~/.codebuddy/projects/**/*.jsonl` session storage and
its Claude Code-derived layout (inspected snapshot: `daa6c55099d34458ff773f6c1edd777f16fb7693`).
resumer only reads the session metadata needed for browsing and resuming.

CodeBuddy's storage format was cross-checked against TokenTracker and its CLI
resume reference. Kimi Code and OpenCode behavior was verified against
kimi-code **2.1.1** and opencode **1.18.33** sources; older releases are
best-effort only (the opencode `storage/` JSON layout of ≤1.0 is kept as a
fallback when no database exists). For real-machine validation steps,
see [docs/real-world-testing.md](docs/real-world-testing.md).

Kimi leaves an opened-but-unused session directory behind (a `state.json` with
no title or prompt, and a `wire.jsonl` carrying only lifecycle events). resumer
skips these, matching Kimi's own session list, so a directory you merely opened
Kimi in does not show up as a session.

resumer also fixes a real-world annoyance: when a session's stored cwd has gone
stale (iCloud/Obsidian path drift), it re-derives the correct project directory
from the session file location, so `codebuddy --resume` actually works.

When a session's working directory has been deleted — the project was renamed,
moved, or removed, or the session data was carried to another machine — that
session cannot be resumed. resumer replaces the project column with
`(deleted)` in both `resumer list` and the picker, and refuses to launch the
agent CLI on Enter, printing the missing path and the command that unblocks it
instead of the CLI's own opaque `created under a different directory` error:

```
error: cannot resume [kimi-code] <session id> — its working directory has been deleted:
       /home/xu/git-repo/my-project
       the agent CLI cannot start there, so resumer did not run the resume command.
       to resume this session, recreate the directory first:
         mkdir -p "/home/xu/git-repo/my-project"
       then run resumer again and select this session.
```

<details>
<summary>Development</summary>

```bash
git clone https://github.com/QuietSugar/resumer.git
cd resumer
go build -o resumer .
./tests/run-qa.sh --no-vhs   # go vet + full test suite (no external deps)
```

The test suite includes PTY-driven integration tests that exercise the real
TUI end to end (picker → filter → select → exec) against fixtures in
`tests/fixtures/`.

Releases are automated: pushing a `v*` tag builds binaries via goreleaser.

Roadmap: Windows support.

</details>

---

MIT © 2026 Jintae Song · QuietSugar — see [LICENSE](LICENSE).
