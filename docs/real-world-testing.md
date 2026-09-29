# Real-machine testing: OpenCode and Kimi Code

This guide tests the OpenCode and Kimi Code providers against the session data already present on your machine. Resumer reads these stores locally; the commands below do not upload or modify the databases/session files. The generated JSON contains session prompts and local paths, so keep it private and do not share it unredacted.

## 1. Build the current branch

The provider changes are on `arena/01a0ec69-resumer`. Clone it into a separate directory, or use an existing checkout of that branch:

```bash
git clone --single-branch --branch arena/01a0ec69-resumer \
  https://github.com/QuietSugar/resumer.git resumer-real-test
cd resumer-real-test

go test ./...
go build -o ./resumer .
```

Check that the tested CLIs are installed and note their versions. The source baselines used during implementation were OpenCode 1.18.33 and Kimi Code 2.1.1; older versions are best-effort.

```bash
opencode --version
kimi --version
```

## 2. Test OpenCode

By default OpenCode data is under `$XDG_DATA_HOME/opencode`, or `$HOME/.local/share/opencode` when `XDG_DATA_HOME` is unset. Set `RESUMER_OPENCODE_DATA` to the directory containing `opencode.db` if your data is stored elsewhere.

```bash
OC_DATA="${XDG_DATA_HOME:-$HOME/.local/share}/opencode"
RESUMER_OPENCODE_DATA="$OC_DATA" \
  ./resumer list --source opencode --json --all \
  > /tmp/resumer-opencode.json
```

Inspect only non-prompt summary fields locally:

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
        "assistant_turns=", s.get("asst_count"),
        "cwd=", s.get("cwd"),
    )
PY
```

For the sample database snapshot used during development, `list --all` showed 10 top-level sessions: there were 11 session rows, none archived, and one child session (non-empty `parent_id`) that should be omitted. In that snapshot, session `ses_fc9a53cc7ffeOFRkRIJulZ2QCw` had three user messages and ten assistant message records. The records shared three `parentID` values, so the expected `asst_count` is **3**, not 10; tool-call continuations count as part of their parent user turn. Your session count may differ if the database has changed.

To verify resume behavior, start the picker and select a session. It should open the corresponding OpenCode session; do not submit a new prompt unless you intend to modify it:

```bash
RESUMER_OPENCODE_DATA="$OC_DATA" ./resumer --source opencode
```

The resume command is `opencode --session <session-id>`.

## 3. Test Kimi Code

Kimi Code stores data in `$HOME/.kimi-code` by default. If you use a custom data directory, set `KIMI_DATA` to that directory. `RESUMER_KIMI_HOME` tells resumer which Kimi data root to inspect.

```bash
KIMI_DATA="${KIMI_CODE_HOME:-$HOME/.kimi-code}"
RESUMER_KIMI_HOME="$KIMI_DATA" \
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
        "assistant_turns=", s.get("asst_count"),
        "cwd=", s.get("cwd"),
        "title=", s.get("title"),
    )
PY
```

Compare session IDs, titles, working directories, timestamps, and first/last prompts with Kimi Code's own session history. Prompt text is included in the JSON file, so review it locally rather than sharing the raw file. The Kimi provider reads `state.json` and the main agent's `agents/main/wire.jsonl`; subagent streams are excluded. In the 2.1.x wire format, completed main-agent turns come from `turn.ended`.

Verify resume behavior by selecting a session in the picker:

```bash
RESUMER_KIMI_HOME="$KIMI_DATA" ./resumer --source kimi-code
```

The resume command is `kimi --session <session-id>`.

## 4. Test CodeBuddy CLI

CodeBuddy session JSONL files are read from `~/.codebuddy/projects/` by default. Set `CODEBUDDY_HOME` for a custom CodeBuddy root, or set `RESUMER_CODEBUDDY_HOME` to override the root for resumer only.

```bash
RESUMER_CODEBUDDY_HOME="${CODEBUDDY_HOME:-$HOME/.codebuddy}" \
  ./resumer list --source codebuddy --json --all \
  > /tmp/resumer-codebuddy.json
```

Compare session IDs, titles, working directories, timestamps, and prompts with CodeBuddy's own session picker. The JSON includes prompt text; review it locally and do not share raw session logs. The provider reads `projects/**/*.jsonl` and does not install hooks or modify CodeBuddy settings. Resume behavior uses `codebuddy --resume <session-id>` as documented by CodeBuddy's CLI reference. Storage-layout research was cross-checked against [TokenTracker](https://github.com/xiufengsun/TokenTracker) (snapshot `daa6c55099d34458ff773f6c1edd777f16fb7693`).

## 5. If results differ

Check that the data root points to the directory containing OpenCode's `opencode.db` or Kimi's `sessions/` and `session_index.jsonl`. Use `--all` to avoid the normal date window hiding older sessions. If session IDs or counts differ from the native CLI, keep the raw JSON and session files private; share only the CLI versions, sanitized error message, and the specific mismatching session ID/count needed to diagnose it.

## Future optimization: large OpenCode databases

The current OpenCode reader is correctness-oriented, not optimized for very large databases. It reads the complete SQLite file into memory, and table scans currently materialize the scanned rows before processing them. Listing also scans message tables to derive first/last-prompt metadata. On databases over 100 MB, this can increase both peak memory use and scan time; removing display-only counts does not avoid that work.

Potential follow-up work:

- Make SQLite table scans stream rows rather than accumulating a complete `[][]Value` in memory.
- Reduce unnecessary row/column decoding while preserving prompt extraction and compatibility with both the durable and legacy message tables.
- Load lightweight session metadata for the initial list, then defer full prompt/detail parsing until a session is selected. This would need to preserve current filtering, JSON output, and preview behavior.

These are future ideas, not implemented optimizations. Benchmark against a representative large database and verify output parity before adopting them.
