# Real-world testing

This guide checks resumer against local session data from all five supported
providers: Claude Code, CodeBuddy, Codex, Kimi Code, and OpenCode. Resumer's
discovery and list operations read these stores locally and do not upload or
modify them. The resume checks launch each agent's native CLI; behavior after
launch is controlled by that CLI and may update its own session data. Generated
JSON may contain prompts and local paths, so keep it private and do not share it
unredacted.

The focus of this revision is **archived and deleted sessions**: what each
provider does with a session the agent has archived or deleted, and whether
resumer can be made to list or resume one anyway. Section 1 is the test matrix
for that, built on the synthetic fixtures in `tests/fixtures/`. Sections 2–4
cover the rest in condensed form.

## Reporting results (required)

Every case in section 1 has an ID, a procedure, and an expected result. When you
run this document, report back with:

1. The build under test — `git rev-parse HEAD` (or branch/tag), `go version`, and
   the version of each agent CLI you exercised.
2. For each case ID: the **actual** output (paste it, do not summarize) and one
   of `PASS`, `FAIL`, `N-A` (with the reason, e.g. "installed CodeBuddy never
   writes `isSidechain`").
3. Anything unexpected, even in a case that passed.

A case passes only when the observed session set matches the expected set
exactly — same session IDs, same count. "Close enough" is a FAIL.

Two self-checks before reporting anything as failing:

- If a baseline command returns `0 sessions`, the data-root override is wrong.
  Check it against the table in 0.2 and stop; do not report the cases below.
- The fixtures' working directories (`/tmp/resumer-fixtures/…`) do not exist on
  most machines, so every row carries the `(deleted)` project label. That is
  expected here and affects no count. For clean labels run
  `mkdir -p /tmp/resumer-fixtures/{alpha,beta,kimi-one,kimi-two,kimi-three,kimi-four,oc-one,oc-two,oc-three,oc-json,codebuddy-alpha,codex-one}`
  first.

Section 5 has a collector that runs every case into one file you can attach,
plus a results table to fill in.

## 0. Build and data roots

### 0.1 Build

```bash
git clone https://github.com/QuietSugar/resumer.git resumer-real-test
cd resumer-real-test
git checkout main            # or the branch under test

go test ./...
go build -o ./resumer .
```

Note the versions of the CLIs you want to exercise. Implementation baselines
were OpenCode 1.18.33 and Kimi Code 2.1.1; older versions are best-effort.
CodeBuddy storage and resume behavior should be checked against the installed
CLI version.

```bash
opencode --version
kimi --version
codebuddy --version
```

### 0.2 Data-root overrides

| provider | `--source` | root override | default root | fixture |
|---|---|---|---|---|
| Claude Code | `claude-code` | `RESUMER_CLAUDE_PROJECT_ROOT` | `~/.claude/projects` | `tests/fixtures/claude-code` |
| CodeBuddy | `codebuddy` | `RESUMER_CODEBUDDY_HOME` (or `CODEBUDDY_HOME`) | `~/.codebuddy` | `tests/fixtures/codebuddy` |
| Codex | `codex` | `RESUMER_CODEX_SESSION_ROOT` + `RESUMER_CODEX_INDEX_FILE` | `~/.codex/sessions` + `~/.codex/session_index.jsonl` | `tests/fixtures/codex` |
| Kimi Code | `kimi-code` | `RESUMER_KIMI_HOME` | `~/.kimi-code` | `tests/fixtures/kimi-home` |
| OpenCode | `opencode` | `RESUMER_OPENCODE_DATA` | `$XDG_DATA_HOME/opencode` | `tests/fixtures/opencode-home` |

These overrides are resumer-only: they change where resumer looks, never where
the agent stores data. `--all` disables the time filter (the CLI default is
already "no limit"; keep `--all` so the intent is explicit).

## 1. Archived and deleted sessions

Each case owns its own store, copied from a fixture, so cases are independent
and re-runnable in any order. **Never mutate a fixture in place** — `go test
./...` reads the same files.

### 1.1 Helpers and stores

```bash
cd /path/to/resumer-real-test
R=/tmp/archdel
rm -rf "$R"; mkdir -p "$R"

fresh() { cp -r "tests/fixtures/$2" "$R/$1"; }

# Counts and IDs only — no prompt text, so output is safe to paste.
summ() {   # summ FILE.json
  python3 - "$1" <<'PY'
import json, sys
rows = json.load(open(sys.argv[1], encoding="utf-8"))
print(len(rows), "sessions")
for r in rows:
    print("   ", r["source"], r["session_id"], "asst=%d" % r["asst_count"],
          "prompts=%d" % len(r["prompts"]))
PY
}

# A1 — kimi: an archived session (state.json archived:true, wire stream, index entry)
fresh kimi-arch kimi-home
KSID=arch0001-1111-7000-8000-000000000009
KD="$R/kimi-arch/sessions/wd_archived-demo_$KSID/$KSID"
mkdir -p "$KD/agents/main"
cp tests/fixtures/kimi-home/sessions/*kimi-one_*/*/agents/main/wire.jsonl "$KD/agents/main/wire.jsonl"
cat > "$KD/state.json" <<EOF
{"id":"$KSID","version":2,"title":"Archived Kimi Session","createdAt":1776232000000,"updatedAt":1776232060000,"archived":true,"cwd":"/tmp/resumer-fixtures/kimi-archived","isCustomTitle":false}
EOF
printf '%s\n' "{\"sessionId\":\"$KSID\",\"sessionDir\":\"$KD\",\"workDir\":\"/tmp/resumer-fixtures/kimi-archived\"}" \
  >> "$R/kimi-arch/session_index.jsonl"

# B4 — kimi: a deleted session (directory gone, index entry kept)
fresh kimi-del kimi-home
rm -rf "$R"/kimi-del/sessions/*kimi-two_*

# A2 — opencode: archived session left over in the pre-1.1 JSON store
fresh oc-arch opencode-home
mkdir -p "$R/oc-arch/storage/session/proj-archived"
cat > "$R/oc-arch/storage/session/proj-archived/ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbb.json" <<'EOF'
{
  "id": "ses_bbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "projectID": "proj-archived",
  "directory": "/tmp/resumer-fixtures/oc-archived",
  "title": "OpenCode Archived Leftover",
  "version": "1.0.0",
  "time": { "created": 1776228000000, "updated": 1776229000000 }
}
EOF

# A2b — opencode: child session left over in the JSON store, naming its parent
fresh oc-child opencode-home
mkdir -p "$R/oc-child/storage/session/proj-child"
cat > "$R/oc-child/storage/session/proj-child/ses_cccccccccccccccccccccccccccc.json" <<'EOF'
{
  "id": "ses_cccccccccccccccccccccccccccc",
  "parentID": "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "projectID": "proj-child",
  "directory": "/tmp/resumer-fixtures/oc-child",
  "title": "OpenCode Child Leftover",
  "version": "1.0.0",
  "time": { "created": 1776229600000, "updated": 1776229700000 }
}
EOF

# B5 — opencode: SQLite row deleted, JSON copy left behind
fresh oc-del opencode-home
python3 - "$R/oc-del/opencode.db" <<'PY'
import sqlite3, sys
con = sqlite3.connect(sys.argv[1])
con.execute("DELETE FROM session WHERE id = ?", ("ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaa",))
con.commit()
PY
mkdir -p "$R/oc-del/storage/session/proj-deleted"
cat > "$R/oc-del/storage/session/proj-deleted/ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaa.json" <<'EOF'
{
  "id": "ses_aaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "projectID": "proj-deleted",
  "directory": "/tmp/resumer-fixtures/oc-deleted",
  "title": "OpenCode Deleted Leftover",
  "version": "1.0.0",
  "time": { "created": 1776229200000, "updated": 1776229500000 }
}
EOF

# B1 — claude-code: session file deleted
fresh cc-del claude-code
rm "$R/cc-del/-fixture-beta/bbbbbbbb-0003-4000-8000-000000000003.jsonl"

# C1 — claude-code: sub-agent (sidechain) records appended to a real session
fresh cc-side claude-code
cat >> "$R/cc-side/-fixture-alpha/aaaaaaaa-0001-4000-8000-000000000001.jsonl" <<'EOF'
{"type":"user","timestamp":"2026-04-15T01:05:00.000Z","isSidechain":true,"message":{"role":"user","content":"SUBAGENT PROMPT MUST NOT COUNT"}}
{"type":"assistant","timestamp":"2026-04-15T01:05:05.000Z","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"subagent reply"}]}}
EOF

# C3 — claude-code: a session file under subagents/ (must not be a top-level session)
fresh cc-sub claude-code
mkdir -p "$R/cc-sub/-fixture-alpha/subagents"
cp "$R/cc-sub/-fixture-alpha/aaaaaaaa-0002-4000-8000-000000000002.jsonl" \
   "$R/cc-sub/-fixture-alpha/subagents/aaaaaaaa-0009-4000-8000-000000000009.jsonl"

# B2 — codebuddy: the only session file deleted (the fixture has exactly one)
fresh cb-del codebuddy
rm "$R"/cb-del/projects/*/*.jsonl

# C2 — codebuddy: sub-agent (sidechain) records appended
fresh cb-side codebuddy
cat >> "$R"/cb-side/projects/*/*.jsonl <<'EOF'
{"type":"message","role":"user","timestamp":1776229300000,"sessionId":"cb111111-1111-4111-8111-111111111111","cwd":"/tmp/resumer-fixtures/codebuddy-alpha","isSidechain":true,"message":{"content":[{"type":"input_text","text":"SUBAGENT PROMPT MUST NOT COUNT"}]}}
{"type":"message","role":"assistant","timestamp":1776229305000,"sessionId":"cb111111-1111-4111-8111-111111111111","isSidechain":true,"message":{"content":[{"type":"output_text","text":"subagent reply"}]}}
EOF

# B3 — codex: rollout deleted, its index entry (with a thread name) kept
fresh cx-del codex
rm "$R/cx-del/2026/04/15/rollout-2026-04-15T05-00-00-019cccc1-1111-7000-8000-000000000001.jsonl"
```

Fixture paths contain long generated names, so **glob them, never hand-type
them** — a typo'd `rm -rf` silently does nothing, or hits the wrong tree.

### 1.2 Expected results

Baselines on the unmodified fixtures: kimi 4, claude-code 5, codebuddy 1,
codex 4, opencode 4 sessions.

| case | store | expected | current build |
|---|---|---|---|
| A1 | `kimi-arch` | 4 sessions, `arch0001-…009` absent | 5 — listed (D1) |
| B4 | `kimi-del` | 3 sessions, `dddd0002-…002` absent | 3 — passes |
| A2 | `oc-arch` | 4 sessions, `ses_bbbb…` absent | 5 — listed (D2a) |
| A2b | `oc-child` | 4 sessions, `ses_cccc…` absent | 4 — passes |
| B5 | `oc-del` | 3 sessions, `ses_aaaa…` absent | 4 — listed (D2b) |
| B1 | `cc-del` | 4 sessions, `bbbbbbbb-0003…` absent | 4 — passes |
| C1 | `cc-side` | 5 sessions, `aaaaaaaa-0001…` stays `prompts=2 asst=2` | `3 / 3` (D3) |
| C3 | `cc-sub` | 5 sessions, `aaaaaaaa-0009…` absent | 5 — passes |
| B2 | `cb-del` | 0 sessions | 0 — passes |
| C2 | `cb-side` | 1 session, `cb111111…` stays `prompts=2 asst=1` | `3 / 2` (D3) |
| B3 | `cx-del` | 3 sessions, `019cccc1-…001` absent | 3 — passes |

### 1.3 A — archived sessions

An archived session is one the agent has put away but not deleted. It must not
be offered for resume.

**A1 — Kimi Code lists archived sessions (defect D1)**

Kimi's `state.json` carries `"archived": true/false`; the Kimi provider does not
read that field, so an archived session is listed like any other.

```bash
RESUMER_KIMI_HOME="$R/kimi-arch" ./resumer list --source kimi-code --all --json > /tmp/kimi-arch.json
summ /tmp/kimi-arch.json
```

- Expected: `4 sessions`; `arch0001-1111-7000-8000-000000000009` absent.
- Current build: `5 sessions`, archived one listed — FAIL (D1).
- Picker check: `RESUMER_KIMI_HOME="$R/kimi-arch" ./resumer --source kimi-code`
  must not show "Archived Kimi Session".

**A2 — OpenCode resurrects an archived session from the legacy JSON store (defect D2a)**

The SQLite filters are correct (`parent_id` set → child, `time_archived` set →
archived), but the merge with the pre-1.1 JSON store only remembers the IDs
SQLite *emitted*, so a leftover JSON file for a skipped session is appended as if
it were live.

```bash
RESUMER_OPENCODE_DATA="$R/oc-arch" ./resumer list --source opencode --all --json > /tmp/oc-arch.json
summ /tmp/oc-arch.json
```

- Expected: `4 sessions`; `ses_bbbb…` absent.
- Current build: `5 sessions`, `ses_bbbb…` listed — FAIL (D2a).

**A2b — a child session in the legacy JSON store is still skipped (regression guard)**

```bash
RESUMER_OPENCODE_DATA="$R/oc-child" ./resumer list --source opencode --all --json > /tmp/oc-child.json
summ /tmp/oc-child.json
```

- Expected: `4 sessions`; `ses_cccc…` absent, because the JSON record names its
  parent. Passes today and must keep passing once D2a is fixed.

**A3 — providers with no archive concept**

Claude Code, CodeBuddy, and Codex expose no archived-session state that resumer
can read, so there is nothing to exclude. Confirm by inspecting the store you
test, mark `N-A`, and say what you found. Do not invent an archive flag.

### 1.4 B — deleted sessions

A deleted session must disappear completely. Resumer discovers sessions by
enumerating files and directories, so a deleted session is normally just gone;
the risk is a stale index or a second copy of the data bringing it back.

**B1 — Claude Code: delete the session file**

```bash
RESUMER_CLAUDE_PROJECT_ROOT="$R/cc-del" ./resumer list --source claude-code --all --json > /tmp/cc-del.json
summ /tmp/cc-del.json
```

- Expected: `4 sessions`; `bbbbbbbb-0003-4000-8000-000000000003` absent. Passes.

**B2 — CodeBuddy: delete the session file**

```bash
RESUMER_CODEBUDDY_HOME="$R/cb-del" ./resumer list --source codebuddy --all --json > /tmp/cb-del.json
summ /tmp/cb-del.json
```

- Expected: `0 sessions`. Passes.

**B3 — Codex: delete the rollout, keep the index entry**

`session_index.jsonl` is a title index only; it must never be able to create a
session on its own.

```bash
RESUMER_CODEX_SESSION_ROOT="$R/cx-del" RESUMER_CODEX_INDEX_FILE="$R/cx-del/session_index.jsonl" \
  ./resumer list --source codex --all --json > /tmp/cx-del.json
summ /tmp/cx-del.json
```

- Expected: `3 sessions`; `019cccc1-1111-7000-8000-000000000001` absent even
  though its index entry (with a thread name) is still there. Passes.
- The fixture index also holds a ghost entry,
  `019cccc9-9999-7000-8000-000000000099`, with no rollout file at all. It must
  never appear in any run, including the ones above.

**B4 — Kimi Code: delete the session directory, keep the index entry**

```bash
RESUMER_KIMI_HOME="$R/kimi-del" ./resumer list --source kimi-code --all --json > /tmp/kimi-del.json
summ /tmp/kimi-del.json
```

- Expected: `3 sessions`; `dddd0002-2222-7000-8000-000000000002` absent although
  its `session_index.jsonl` entry is still present. Passes.

**B5 — OpenCode: delete the SQLite row, leave a JSON copy (defect D2b)**

```bash
RESUMER_OPENCODE_DATA="$R/oc-del" ./resumer list --source opencode --all --json > /tmp/oc-del.json
summ /tmp/oc-del.json
```

- Expected: `3 sessions`; `ses_aaaa…` absent.
- Current build: `4 sessions`, `ses_aaaa…` listed — FAIL.
- Note for the report: fixing D2a (recording every ID SQLite *scanned*, not just
  the ones it emitted) does **not** fix this case, because a deleted row is never
  scanned. See 1.7.

### 1.5 C — sub-agent and sidechain records (related)

Sub-agent traffic must not be counted as the main session's prompts or assistant
turns. Neither the Claude Code nor the CodeBuddy provider reads `isSidechain`, so
sub-agent records inflate `prompts` and `asst_count` (defect D3).

**C1 — Claude Code**

```bash
RESUMER_CLAUDE_PROJECT_ROOT="$R/cc-side" ./resumer list --source claude-code --all --json > /tmp/cc-side.json
summ /tmp/cc-side.json
```

- Expected: `5 sessions`, and for `aaaaaaaa-0001-4000-8000-000000000001`
  `prompts=2` and `asst_count=2` — the fixture's own values, pinned by
  `internal/provider/claudecode.TestFixtureParsing`. The string
  `SUBAGENT PROMPT MUST NOT COUNT` must not appear anywhere in the output.
- Current build: `prompts=3`, `asst_count=3` — FAIL (D3).

**C2 — CodeBuddy**

```bash
RESUMER_CODEBUDDY_HOME="$R/cb-side" ./resumer list --source codebuddy --all --json > /tmp/cb-side.json
summ /tmp/cb-side.json
```

- Expected: `1 session`, and for `cb111111-1111-4111-8111-111111111111`
  `prompts=2` and `asst_count=1` — pinned by
  `internal/provider/codebuddy.TestFixtureParsing`.
- Current build: `prompts=3`, `asst_count=2` — FAIL (D3).
- If your CodeBuddy version never writes `isSidechain`, mark `N-A` and say so.
  That is useful information, not a pass.

**C3 — session files under `subagents/` are not top-level sessions**

```bash
RESUMER_CLAUDE_PROJECT_ROOT="$R/cc-sub" ./resumer list --source claude-code --all --json > /tmp/cc-sub.json
summ /tmp/cc-sub.json
```

- Expected: `5 sessions`; `aaaaaaaa-0009-4000-8000-000000000009` absent. Passes.

### 1.6 D — deleted working directory (the session still exists)

A different failure mode: the session is present, but the directory it was
created in is gone. Resumer labels the row `(deleted)`, refuses to resume, and
prints the command that unblocks it. Automated coverage: `internal/cwd`
(`TestResolve`, `TestResolveRecoversStaleStoredCwd`,
`TestResolveUnknownCwdIsNotRefused`), `internal/render`
(`TestRenderersFlagMissingWorkingDirectory`), `tests/integration`
(`TestUnifiedRender`, `TestSelectRefusesWhenWorkingDirectoryIsGone`).

Manual check with a fixture session whose cwd (`/tmp/resumer-fixtures/alpha`)
does not exist:

```bash
./resumer list --source claude-code --all     # the row is labeled "(deleted)"
./resumer --source claude-code                # select that session, press Enter
```

- Expected: the list labels the row `(deleted)`; on Enter resumer prints
  `error: cannot resume [claude-code] <id> — its working directory has been
  deleted:`, the path, and `mkdir -p "<path>"`, then exits with status 3 without
  launching the agent CLI.
- After `mkdir -p /tmp/resumer-fixtures/alpha` the label disappears and Enter
  resumes normally.
- A session with no recorded cwd keeps showing `(unknown)` and is **not**
  refused — that is section 3.2.

### 1.7 Known defects and open questions

| ID | provider | symptom | status |
|---|---|---|---|
| D1 | kimi-code | archived sessions are listed and offered for resume | open |
| D2a | opencode | archived/child sessions resurrect from the legacy JSON store | open |
| D2b | opencode | a session whose SQLite row was deleted resurrects from a JSON copy | open |
| D3 | claude-code, codebuddy | `isSidechain` records count as the main session's prompts/turns | open |

**Open question behind D2b (case B5).** The pre-1.1 JSON store is legitimate
data: a session that exists only there (the fixture's `ses_jjjj`) must still be
listed. But a JSON copy of a session whose SQLite row is gone is
indistinguishable from that legitimate case using the data alone, so recording
scanned IDs — the fix for D2a — cannot fix D2b. Candidate policies, to be
decided before B5 can pass:

1. Ignore the JSON store entirely when `opencode.db` exists. Simple, but drops
   legitimate JSON-only sessions that were never migrated.
2. Treat a JSON session as stale when its `time.updated` predates the database
   file's creation. Cheap heuristic; fragile across clock skew and file copies.
3. Ask the agent's own data (for example a deletion tombstone) instead of
   guessing. Correct if such a marker exists — needs verification against the
   installed OpenCode version.

Until one is chosen, report B5 as FAIL with the note "blocked on policy
decision", not as a plain defect.

## 2. Condensed regression — previously verified behavior

These behaviors have been verified repeatedly and are pinned by automated tests.
Run `go test ./...` instead of re-doing them by hand; the manual column is a
one-minute sanity check only.

| behavior | automated coverage | manual check |
|---|---|---|
| merged list renders every active provider with badges | `tests/integration.TestUnifiedRender` | `./resumer list --all` shows `[cc] [cb] [codex] [kimi] [opencode]` rows |
| deleted working directory labeled, resume refused, `mkdir -p` printed, exit 3 | `internal/cwd.*`, `internal/render.TestRenderersFlagMissingWorkingDirectory`, `tests/integration.TestSelectRefusesWhenWorkingDirectoryIsGone` | 1.6 |
| unknown cwd shows `(unknown)` and is not refused | `internal/cwd.TestResolveUnknownCwdIsNotRefused`, `internal/provider/kimi.TestSessionWithoutIndexEntryOrWire` | pick a kimi session with no recorded cwd |
| archived + child sessions skipped in SQLite | `internal/provider/opencode.TestArchivedAndChildSkipped` | A2 / A2b |
| provider enable/disable; a disabled provider is not scanned | `internal/provider.TestActiveFiltersDisabledProviders`, `internal/cli.TestProviderCmdOffListOn` | `./resumer provider off kimi-code`, then `./resumer list --all` |
| date / project / limit filters | `internal/provider/*.TestDateFilter`, `TestProjectFilter`, `TestFilters` | `./resumer list --days 7` |
| picker sort toggle and source cycling | `tests/integration.TestSortToggleAndSourceCycle` | press the sort key, then cycle source |
| resume exec per provider | `tests/integration.Test{Codex,CodeBuddy,Kimi,OpenCode}SelectExec` | select one session per provider |
| UI text is English, including the `(deleted)` label | `internal/textutil.DirDeletedLabel` and the render tests | eyeball the list |

Smoke test against real data — one command per provider, then compare with the
agent's own session picker:

```bash
./resumer list --source claude-code --all
./resumer list --source codebuddy  --all
./resumer list --source codex      --all
./resumer list --source kimi-code  --all
./resumer list --source opencode   --all
```

Compare session IDs, titles, working directories, timestamps, and first/last
prompts with each agent's own history. Child/subagent sessions and archived
sessions are intentionally omitted. A different CLI version, an unusual session
layout, or an incomplete session can all explain a mismatch — record the version
you saw.

## 3. Real-machine verification still open

### 3.1 Custom Kimi data root (`KIMI_CODE_HOME`)

resumer's Kimi provider resolves its data root as `RESUMER_KIMI_HOME`, else
`$HOME/.kimi-code`; it never reads kimi's own `KIMI_CODE_HOME`. On a machine
where the data lives elsewhere the two disagree:

| | reads sessions from |
|---|---|
| kimi | `$KIMI_CODE_HOME/sessions` |
| resumer | `$HOME/.kimi-code/sessions` |

Symptom: resumer lists no kimi sessions, or only stale ones, while `kimi` shows a
full history. To confirm:

```bash
export KIMI_CODE_HOME=/path/to/custom/kimi-data
ls "$KIMI_CODE_HOME/sessions"                     # kimi's view — populated
./resumer --source kimi-code --all                # resumer's view — empty or stale
```

If confirmed, the fix is a precedence change: consult `KIMI_CODE_HOME` before
falling back to `$HOME/.kimi-code`, keeping `RESUMER_KIMI_HOME` as the explicit
override. Deliberately not applied yet.

### 3.2 Kimi sessions with no recorded cwd

Some kimi sessions carry no working directory anywhere resumer can read: no
`cwd`/`workDir` in `state.json`, no matching `session_index.jsonl` entry. Those
rows show `(unknown)`, and Enter still runs `kimi --session <id>` from wherever
resumer was started. Whether kimi accepts that, or fails with
`Session "…" was created under a different directory`, depends on where kimi
records the directory. Find such a session and report which happens; if kimi
fails, find where the directory is recorded and teach the provider to read it.

## 4. Interpreting `asst_count`

`asst_count` is a provider-defined estimate of assistant-side activity, not an
exact turn count. Providers count different things (assistant messages,
completed turns, events) and the result may be incomplete. Treat it as a rough
conversation-size cue only — never as a cross-provider comparison, and never as
a pass/fail criterion against an agent's native UI. Zero means "no countable
activity found", not "empty session". The picker shows it with `~` for that
reason. The exact values in 1.5 are asserted because they are pinned by the
provider's own fixture test, not because the number is meaningful in itself.

## 5. Results

### 5.1 Collector

Builds nothing and changes nothing. It re-runs every case against the stores
from 1.1 and writes one file to attach to your report.

```bash
cat > /tmp/collect-archive-delete.sh <<'EOS'
#!/usr/bin/env bash
set -uo pipefail
cd "$(git rev-parse --show-toplevel)"
R=/tmp/archdel
OUT=${1:-/tmp/resumer-archive-delete-results.txt}
: > "$OUT"

summ() {
  python3 -c '
import json, sys
rows = json.load(sys.stdin)
print(len(rows), "sessions")
for r in rows:
    print("   ", r["source"], r["session_id"], "asst=%d" % r["asst_count"],
          "prompts=%d" % len(r["prompts"]))
'
}

run() {   # run LABEL ENVASSIGN... -- ./resumer args...
  local label="$1"; shift
  {
    echo "===== $label"
    env "$@" 2>"$OUT.err" | summ
    if [ -s "$OUT.err" ]; then echo "--- stderr ---"; cat "$OUT.err"; fi
    echo
  } | tee -a "$OUT"
}

run "A1  kimi archived      (expect 4, no arch0001...009)" \
    RESUMER_KIMI_HOME="$R/kimi-arch" ./resumer list --source kimi-code --all --json
run "B4  kimi deleted       (expect 3, no dddd0002...002)" \
    RESUMER_KIMI_HOME="$R/kimi-del" ./resumer list --source kimi-code --all --json
run "A2  opencode archived  (expect 4, no ses_bbbb)" \
    RESUMER_OPENCODE_DATA="$R/oc-arch" ./resumer list --source opencode --all --json
run "A2b opencode child     (expect 4, no ses_cccc)" \
    RESUMER_OPENCODE_DATA="$R/oc-child" ./resumer list --source opencode --all --json
run "B5  opencode deleted   (expect 3, no ses_aaaa)" \
    RESUMER_OPENCODE_DATA="$R/oc-del" ./resumer list --source opencode --all --json
run "B1  claude-code deleted(expect 4, no bbbbbbbb-0003)" \
    RESUMER_CLAUDE_PROJECT_ROOT="$R/cc-del" ./resumer list --source claude-code --all --json
run "C1  claude-code sidechain (aaaa...0001 must stay prompts=2 asst=2)" \
    RESUMER_CLAUDE_PROJECT_ROOT="$R/cc-side" ./resumer list --source claude-code --all --json
run "C3  claude-code subagents/ (expect 5, no aaaaaaaa-0009)" \
    RESUMER_CLAUDE_PROJECT_ROOT="$R/cc-sub" ./resumer list --source claude-code --all --json
run "B2  codebuddy deleted  (expect 0)" \
    RESUMER_CODEBUDDY_HOME="$R/cb-del" ./resumer list --source codebuddy --all --json
run "C2  codebuddy sidechain (cb111111 must stay prompts=2 asst=1)" \
    RESUMER_CODEBUDDY_HOME="$R/cb-side" ./resumer list --source codebuddy --all --json
run "B3  codex deleted      (expect 3, no 019cccc1...001, never 019cccc9...099)" \
    RESUMER_CODEX_SESSION_ROOT="$R/cx-del" RESUMER_CODEX_INDEX_FILE="$R/cx-del/session_index.jsonl" \
    ./resumer list --source codex --all --json

echo "written to $OUT"
EOS
bash /tmp/collect-archive-delete.sh
```

If a store is missing, re-run the 1.1 block that builds it first.

### 5.2 Results table

Copy this into your report; paste the actual output below the table or attach
the collector's file.

| case | expected | actual (paste) | PASS / FAIL / N-A | notes |
|---|---|---|---|---|
| A1 kimi archived | 4, `arch0001-…009` absent | | | |
| A2 opencode archived leftover | 4, `ses_bbbb…` absent | | | |
| A2b opencode child leftover | 4, `ses_cccc…` absent | | | |
| A3 no-archive providers | nothing to exclude | | | |
| B1 claude-code deleted | 4, `bbbbbbbb-0003…` absent | | | |
| B2 codebuddy deleted | 0 | | | |
| B3 codex deleted rollout | 3, `019cccc1…001` absent | | | |
| B4 kimi deleted dir | 3, `dddd0002…002` absent | | | |
| B5 opencode deleted row | 3, `ses_aaaa…` absent | | | blocked on policy |
| C1 claude-code sidechain | `aaaaaaaa-0001…` stays 2 / 2 | | | |
| C2 codebuddy sidechain | `cb111111…` stays 2 / 1 | | | |
| C3 `subagents/` skipped | 5, `aaaaaaaa-0009…` absent | | | |
| D deleted working directory | `(deleted)`, exit 3, `mkdir -p` | | | |
| `go test ./...` | all packages pass | | | |
| picker spot check | archived/deleted sessions not offered | | | |

Build under test: `git rev-parse HEAD` =
`go version` =
Agent CLI versions: opencode
, kimi
, codebuddy

For a bug report, share the build, the CLI versions, a sanitized error message,
and only the minimum session ID or count needed to diagnose the issue; redact
prompts, local paths, and credentials.

## Appendix: large OpenCode databases

The current OpenCode reader is correctness-oriented, not optimized for very
large databases. It reads the complete SQLite file into memory, and table scans
materialize scanned rows before processing them. Listing also scans message
tables to derive prompt metadata and the assistant-activity estimate. On
databases over 100 MB this can increase peak memory use and scan time; changing
how the estimate is displayed alone would not avoid that work.

Potential follow-up work:

- Make SQLite table scans stream rows rather than accumulating a complete
  `[][]Value` in memory.
- Reduce unnecessary row/column decoding while preserving prompt extraction and
  compatibility with both the durable and legacy message tables.
- Load lightweight session metadata for the initial list, then defer full
  prompt/detail parsing until a session is selected. This would need to preserve
  current filtering, JSON output, and preview behavior.

These are future ideas, not implemented optimizations. Benchmark against a
representative large database and verify output parity before adopting them.
