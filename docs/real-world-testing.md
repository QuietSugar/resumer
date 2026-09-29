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

## 4. If results differ

Check that the data root points to the directory containing OpenCode's `opencode.db` or Kimi's `sessions/` and `session_index.jsonl`. Use `--all` to avoid the normal date window hiding older sessions. If session IDs or counts differ from the native CLI, keep the raw JSON and session files private; share only the CLI versions, sanitized error message, and the specific mismatching session ID/count needed to diagnose it.
