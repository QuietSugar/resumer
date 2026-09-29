# resumer

> Browse & resume your Claude Code / Codex sessions — one picker, zero dependencies.

![resumer demo](docs/demo.gif)

A single static binary. No Python, no fzf, no setup. Open the picker, see every
recent AI-CLI session across providers with a full preview, hit Enter, and you're
back in the conversation.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/jin-ttao/resumer/main/install.sh | sh
```

or with Go:

```bash
go install github.com/jin-ttao/resumer@latest
```

macOS & Linux (arm64/amd64). Windows is on the roadmap.

## Usage

```bash
resumer              # interactive picker
resumer list         # plain list, last 7 days
resumer --help       # everything else
```

Picker keys: `↑↓` browse · `/` filter · `tab` cycle source · `ctrl-s` toggle sort ·
`enter` resume · `esc` cancel.

Useful flags (both picker and `list`): `--days N`, `--date YYYY-MM-DD`, `--all`,
`--project foo`, `--source claude-code|codex|kimi-code|opencode`, `--limit N`. List mode adds
`--json` and `--full [N]`.

## Providers

| Provider | Session source | Resume command |
|---|---|---|
| Claude Code | `~/.claude/projects/**/*.jsonl` | `claude --resume <id>` |
| Codex CLI | `~/.codex/sessions/**/rollout-*.jsonl` | `codex resume <id>` |
| Kimi Code CLI | `~/.kimi-code/sessions/<workDirKey>/<id>/` | `kimi --session <id>` |
| OpenCode | `~/.local/share/opencode/opencode.db` (≤1.0: `storage/session/`) | `opencode --session <id>` |
| Gemini CLI | roadmap | |

Verified against kimi-code **2.1.1** and opencode **1.18.33** sources; older
releases are best-effort only (the opencode `storage/` JSON layout of ≤1.0 is
kept as a fallback when no database exists).

resumer also fixes a real-world annoyance: when a session's stored cwd has gone
stale (iCloud/Obsidian path drift), it re-derives the correct project directory
from the session file location, so `claude --resume` actually works.

<details>
<summary>Development</summary>

```bash
git clone https://github.com/jin-ttao/resumer.git
cd resumer
go build -o resumer .
./tests/run-qa.sh --no-vhs   # go vet + full test suite (no external deps)
```

The test suite includes PTY-driven integration tests that exercise the real
TUI end to end (picker → filter → select → exec) against fixtures in
`tests/fixtures/`.

Releases are automated: pushing a `v*` tag builds binaries via goreleaser.

Roadmap: Gemini provider · Windows support.

</details>

---

Built by [@jin-ttao](https://github.com/jin-ttao). If this helped, leaving a ⭐ helps others find it.
