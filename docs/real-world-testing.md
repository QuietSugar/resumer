# Real-world testing

This guide checks resumer against local OpenCode, Kimi Code, and CodeBuddy session data. Resumer's discovery and list operations read these stores locally and do not upload or modify them. The resume checks launch each agent's native CLI; behavior after launch is controlled by that CLI and may update its own session data. Generated JSON may contain prompts and local paths, so keep it private and do not share it unredacted.

## Interpreting `asst_count`

The JSON field `asst_count` is a provider-defined estimate of assistant-side activity. Providers may count different things (for example, assistant messages, completed turns, or events), and the result may be incomplete or inaccurate. It is intended only as a rough indication of conversation size, not an exact turn count, token count, quality measure, or cross-provider comparison. A value of zero means the provider found no countable activity; it does not necessarily mean the session contains no useful content.

The picker uses this estimate only as part of a coarse conversation-volume marker. In the detail preview it is shown with `~` to signal the approximation. User prompts are extracted separately. When validating real data, check that session discovery and prompt extraction are sensible, but do not treat exact `asst_count` parity with an agent's native UI as a pass/fail criterion.

## 1. Build the current branch

Clone the repository and check out what you want to test (`main` tracks the
latest release):

```bash
git clone https://github.com/QuietSugar/resumer.git resumer-real-test
cd resumer-real-test
git checkout main

go test ./...
go build -o ./resumer .
```

Check that the CLIs you want to test are installed and note their versions. The implementation baselines were OpenCode 1.18.33 and Kimi Code 2.1.1; older versions are best-effort. CodeBuddy storage and resume behavior should be checked against the installed CLI version.

```bash
opencode --version
kimi --version
codebuddy --version
```

## 2. Test OpenCode

By default, OpenCode data is under `$XDG_DATA_HOME/opencode`, or `$HOME/.local/share/opencode` when `XDG_DATA_HOME` is unset. Set `RESUMER_OPENCODE_DATA` to the directory containing `opencode.db` if your data is stored elsewhere.

```bash
OC_DATA="${XDG_DATA_HOME:-$HOME/.local/share}/opencode"
RESUMER_OPENCODE_DATA="$OC_DATA" \
  ./resumer list --source opencode --json --all \
  > /tmp/resumer-opencode.json
```

Inspect counts and paths locally without printing prompts:

```bash
python3 - /tmp/resumer-opencode.json <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as f:
    sessions = json.load(f)
print(f"OpenCode sessions: {len(sessions)}")
for s in sessions:
    print(
        s.get("session_id"),
        "assistant_activity_estimate=", s.get("asst_count"),
        "user_prompts=", len(s.get("prompts", [])),
        "cwd=", s.get("cwd"),
    )
PY
```

Compare the set of visible top-level sessions, IDs, timestamps, project directories, and prompts with OpenCode's own session history. Child/subagent sessions and archived sessions are intentionally omitted. OpenCode currently groups assistant continuations by their parent user message when that relationship is available; treat the resulting `asst_count` as an approximate volume indicator, not an exact cross-provider standard.

To verify resume behavior, start the picker and select a session. It should open the corresponding OpenCode session; do not submit a new prompt unless you intend to modify it:

```bash
RESUMER_OPENCODE_DATA="$OC_DATA" ./resumer --source opencode
```

The resume command is `opencode --session <session-id>`.

## 3. Test Kimi Code

Kimi Code stores data in `$HOME/.kimi-code` by default. If you use a custom data directory, set `KIMI_CODE_HOME` to that directory. `RESUMER_KIMI_HOME` tells resumer which Kimi data root to inspect, and overrides `KIMI_CODE_HOME` for resumer only.

```bash
RESUMER_KIMI_HOME="${KIMI_CODE_HOME:-$HOME/.kimi-code}" \
  ./resumer list --source kimi-code --json --all \
  > /tmp/resumer-kimi.json
```

Print a local summary without printing prompts:

```bash
python3 - /tmp/resumer-kimi.json <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as f:
    sessions = json.load(f)
print(f"Kimi Code sessions: {len(sessions)}")
for s in sessions:
    print(
        s.get("session_id"),
        "assistant_activity_estimate=", s.get("asst_count"),
        "user_prompts=", len(s.get("prompts", [])),
        "cwd=", s.get("cwd"),
    )
PY
```

Compare session IDs, titles, working directories, timestamps, and first/last prompts with Kimi Code's own session history. Prompt text is included in the JSON file, so review it locally rather than sharing the raw file. The Kimi provider reads `state.json` and the main agent's `agents/main/wire.jsonl`; subagent streams are excluded. In the 2.1.x wire format, `turn.ended` events are used for assistant activity when available; older data may require a different fallback. This is one provider's implementation choice, not a promise that its count matches other providers.

Verify resume behavior by selecting a session in the picker:

```bash
RESUMER_KIMI_HOME="${KIMI_CODE_HOME:-$HOME/.kimi-code}" ./resumer --source kimi-code
```

The resume command is `kimi --session <session-id>`.

### Custom Kimi data root (`KIMI_CODE_HOME`) — needs real-machine verification

resumer's Kimi provider resolves its data root like this (`kimiHome()` in
`internal/provider/kimi/kimi.go`):

1. `RESUMER_KIMI_HOME`, if set — a resumer-only override used by the test suite
2. otherwise `$HOME/.kimi-code`

It never reads kimi's own `KIMI_CODE_HOME`. On a machine where the kimi data
lives somewhere else, kimi and resumer therefore disagree about where the
sessions are:

| | reads sessions from |
|---|---|
| kimi | `$KIMI_CODE_HOME/sessions` |
| resumer | `$HOME/.kimi-code/sessions` |

The visible symptom is resumer listing no kimi sessions, or only stale ones,
while `kimi` itself shows a full history. The commands in this section work
around it by passing `KIMI_CODE_HOME` through to `RESUMER_KIMI_HOME`.

To confirm on a real machine:

```bash
export KIMI_CODE_HOME=/path/to/custom/kimi-data

# kimi's own view — should be populated
ls "$KIMI_CODE_HOME/sessions"

# resumer's view, without the workaround — expected to come up empty or stale
./resumer --source kimi-code --all
```

If that is what you see, the fix is a precedence change in `kimiHome()`: consult
`KIMI_CODE_HOME` before falling back to `$HOME/.kimi-code`, keeping
`RESUMER_KIMI_HOME` as the explicit override. It is deliberately not applied
yet — this section is the ticket for it.

### Kimi sessions without a recorded cwd (needs real-machine verification)

Some kimi sessions carry no working directory anywhere resumer can read: no
`cwd`/`workDir` in `state.json`, and no matching entry in `session_index.jsonl`.
resumer cannot invent a path it was never given, so those rows show `(unknown)`
in the project column and Enter still runs `kimi --session <id>` from whatever
directory resumer was started in. Whether kimi accepts that, or fails with
`Session "..." was created under a different directory`, depends on where kimi
itself keeps the session's directory.

The automated suite only pins the current behavior:
`TestSessionWithoutIndexEntryOrWire` (empty cwd, `(unknown)` label) and
`TestResolveUnknownCwdIsNotRefused` (unknown is not treated as deleted).

To close this on a real machine, find such a session and report which of these
holds:

```bash
# 1. Does the session really have no recorded directory?
jq '{cwd, workDir}' ~/.kimi-code/sessions/*/<session-id>/state.json
grep '<session-id>' ~/.kimi-code/session_index.jsonl

# 2. What does the picker show in the project column, and what happens on Enter?
RESUMER_KIMI_HOME="$HOME/.kimi-code" ./resumer --source kimi-code
```

- If kimi resumes successfully from an unrelated cwd, nothing to do.
- If kimi fails, find where kimi records the directory (check
  `~/.kimi-code/sessions/<workDirKey>/<session-id>/` and the CLI's own logs)
  and teach the provider to read it, so the row can be labeled and the resume
  can be refused the way a deleted directory is.

## 4. Test CodeBuddy CLI

CodeBuddy session JSONL files are read from `~/.codebuddy/projects/` by default. Set `CODEBUDDY_HOME` for a custom CodeBuddy root, or set `RESUMER_CODEBUDDY_HOME` to override the root for resumer only.

```bash
RESUMER_CODEBUDDY_HOME="${CODEBUDDY_HOME:-$HOME/.codebuddy}" \
  ./resumer list --source codebuddy --json --all \
  > /tmp/resumer-codebuddy.json
```

Compare session IDs, titles, working directories, timestamps, and prompts with CodeBuddy's own session picker. The parser accepts plain text plus `text`, `input_text`, and `output_text` content blocks. It skips JSONL files under `subagents/`, which are not listed as resumable top-level sessions. The current `asst_count` implementation counts assistant message records; records do not necessarily correspond one-to-one with completed turns, so use the number only as a rough size cue.

The provider reads session data only and does not install hooks or modify CodeBuddy settings. Resume behavior uses `codebuddy --resume <session-id>` as documented by CodeBuddy's CLI reference. Storage-layout research was cross-checked against [TokenTracker](https://github.com/xiufengsun/TokenTracker) (snapshot `daa6c55099d34458ff773f6c1edd777f16fb7693`).

## 5. If results differ

Check that each data-root override points to the expected store: OpenCode's `opencode.db`, Kimi's `sessions/` and `session_index.jsonl`, or CodeBuddy's `projects/` directory. Use `--all` to avoid the normal date window hiding older sessions. Different CLI versions, session layouts, incomplete sessions, and provider-specific counting rules can all affect results.

Keep raw JSON, prompts, and session files private. For a bug report, share CLI versions, a sanitized error message, and only the minimum session ID or count needed to diagnose the issue; redact prompts, local paths, and credentials.

## Future optimization: large OpenCode databases

The current OpenCode reader is correctness-oriented, not optimized for very large databases. It reads the complete SQLite file into memory, and table scans currently materialize scanned rows before processing them. Listing also scans message tables to derive prompt metadata and the assistant-activity estimate. On databases over 100 MB, this can increase peak memory use and scan time; changing how the estimate is displayed alone would not avoid that work.

Potential follow-up work:

- Make SQLite table scans stream rows rather than accumulating a complete `[][]Value` in memory.
- Reduce unnecessary row/column decoding while preserving prompt extraction and compatibility with both the durable and legacy message tables.
- Load lightweight session metadata for the initial list, then defer full prompt/detail parsing until a session is selected. This would need to preserve current filtering, JSON output, and preview behavior.

These are future ideas, not implemented optimizations. Benchmark against a representative large database and verify output parity before adopting them.
