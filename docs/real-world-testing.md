# Real-world testing

This guide checks resumer against local session data from all five supported
providers: CodeBuddy, Kimi Code, and OpenCode. Resumer's
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
  `mkdir -p /tmp/resumer-fixtures/{kimi-one,kimi-two,kimi-three,kimi-four,oc-one,oc-two,oc-three,oc-json,codebuddy-alpha}` and
  `mkdir -p "/tmp/resumer-fixtures/obsidian path with space/vault"` first.

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
| CodeBuddy | `codebuddy` | `RESUMER_CODEBUDDY_HOME` (or `CODEBUDDY_HOME`) | `~/.codebuddy` | `tests/fixtures/codebuddy` |
| Kimi Code | `kimi-code` | `RESUMER_KIMI_HOME` | `~/.kimi-code` | `tests/fixtures/kimi-home` |
| OpenCode | `opencode` | `RESUMER_OPENCODE_DATA` | `$XDG_DATA_HOME/opencode` | `tests/fixtures/opencode-home` |

These overrides are resumer-only: they change where resumer looks, never where
the agent stores data. `--all` disables the time filter (the CLI default is
already "no limit"; keep `--all` so the intent is explicit). `resumer list`
without `--json` now groups rows by workspace; keep using `--json` for the
byte-stable flat array, or pass `--no-group` for the flat table (see 0.4).

### 0.3 Provider availability

A provider is skipped entirely — including by an explicit `--source` — when it
reports itself unavailable, and you get an error instead of a list:

```
error: <provider> provider not available (binary or session directory missing)
```

That is not a "0 sessions" result and none of the cases below apply. The rule
differs per provider:

| provider | data dir | agent binary |
|---|---|---|
| codebuddy | required | not checked |
| kimi-code | required | required |
| opencode | required (`opencode.db` or `storage/`) | required |

So kimi-code and opencode need the agent's CLI on `PATH`. If you do not
have it installed — or do not want the real CLI launched by a resume test —
point resumer at the mock CLIs the repository ships for QA:

```bash
MB=$(git rev-parse --show-toplevel)/tests/mock-bin
export RESUMER_KIMI_BIN="$MB/kimi"
export RESUMER_OPENCODE_BIN="$MB/opencode"
```

The mocks record their invocation (`pwd` and arguments) to
`/tmp/resumer-qa/<name>-mock.log` and exit, so a resume test can assert the
command without starting the agent. Every command in section 1 works with the
mocks in place; only the picker checks in 1.6 need a real CLI if you want to see
the agent actually open a session.

### 0.4 Workspace grouping

`resumer list` (no `--json`) groups sessions by workspace: sessions whose stored
working directory is the same are grouped under one
`── <dir>  ·  N sessions  ·  <providers>` header, regardless of provider. The
grouping identity prefers a provider-native workspace/project id and otherwise
falls back to the working directory:

- **Kimi Code**: the `wd_<slug>_<sha>` session bucket is the native id; when
  `workspaces.json` records a `root`, that directory is the group root.
- **OpenCode**: `session.workspace_id`, else `session.project_id`. The literal
  `"global"` is a sentinel (not a real project) and is treated as "no native id".
- Anything with neither a directory nor a native id is shown under
  `(no workspace)`, scoped per provider.

`--json` output is always a flat array; each session carries nullable
`workspace_id` / `workspace_root` fields so downstream consumers can group
themselves. `resumer list --no-group` restores the flat table — the
pre-grouping layout kept for byte-stable comparisons.

```bash
# Fixtures use one cwd per provider, so each group holds a single session.
./resumer list --all | grep -c '^── '      # = number of distinct fixture cwds
./resumer list --all --no-group | head -3  # flat header + divider + first row
```

To see a cross-provider merge, give two providers a session in the same
directory (e.g. point a kimi `state.json` `cwd` and an opencode `session.directory`
at `/tmp/resumer-fixtures/alpha`); both rows then share one group header. The
pure grouping rules are pinned by `internal/workspace/workspace_test.go`, the
grouped table by `internal/render` tests, and the TUI header/navigation behavior
by `internal/tui/model_test.go`.

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

# B2 — codebuddy: every session file deleted
fresh cb-del codebuddy
rm "$R"/cb-del/projects/*/*.jsonl

# C2 — codebuddy: sub-agent (sidechain) records appended to the alpha session
fresh cb-side codebuddy
cat >> "$R"/cb-side/projects/-tmp-resumer-fixtures-codebuddy-alpha/cb111111-1111-4111-8111-111111111111.jsonl <<'EOF'
{"type":"message","role":"user","timestamp":1776229300000,"sessionId":"cb111111-1111-4111-8111-111111111111","cwd":"/tmp/resumer-fixtures/codebuddy-alpha","isSidechain":true,"message":{"content":[{"type":"input_text","text":"SUBAGENT PROMPT MUST NOT COUNT"}]}}
{"type":"message","role":"assistant","timestamp":1776229305000,"sessionId":"cb111111-1111-4111-8111-111111111111","isSidechain":true,"message":{"content":[{"type":"output_text","text":"subagent reply"}]}}
EOF

```

Fixture paths contain long generated names, so **glob them, never hand-type
them** — a typo'd `rm -rf` silently does nothing, or hits the wrong tree.

### 1.2 Expected results

Baselines on the unmodified fixtures: kimi 4, codebuddy 2, opencode 3 sessions.
(The kimi fixture also holds
one opened-but-unused session — state.json with no title/prompt and a
lifecycle-only wire stream — which resumer skips like Kimi's own surfaces do;
see §0.4 and the unused-session row in §2.)

The OpenCode baseline is 3, not 4, because the fixture also holds a pre-1.1
JSON-only session (`ses_jjjj…`) that is no longer merged in beside a database —
see the policy in 1.7. It is still listed from a store without `opencode.db`:
`internal/provider/opencode.TestJSONFallbackSession` builds one.

The expectations below describe the intended behavior. All of them should pass;
a failure is a regression, not a known issue.

| case | store | expected | covers |
|---|---|---|---|
| A1 | `kimi-arch` | 4 sessions, `arch0001-…009` absent | D1 — fixed |
| B4 | `kimi-del` | 3 sessions, `dddd0002-…002` absent | — |
| A2 | `oc-arch` | 3 sessions, `ses_bbbb…` absent | D2a — fixed |
| A2b | `oc-child` | 3 sessions, `ses_cccc…` absent | — |
| B5 | `oc-del` | 2 sessions, `ses_aaaa…` absent | D2b — fixed |
| B2 | `cb-del` | 0 sessions | — |
| C2 | `cb-side` | 2 sessions, `cb111111…` stays `prompts=2 asst=1` | D3 — fixed |

### 1.3 A — archived sessions

An archived session is one the agent has put away but not deleted. It must not
be offered for resume.

**A1 — Kimi Code: archived sessions are skipped (defect D1, fixed)**

Kimi's `state.json` carries `"archived": true/false`, and Kimi's own session
picker hides archived sessions from its list. The provider now reads the flag and
skips an archived session in both `ListSessions` and `LoadDetail`, so it is
neither listed nor loadable. The session directory and its wire stream stay on
disk untouched — archiving in Kimi is not deleting.

```bash
RESUMER_KIMI_HOME="$R/kimi-arch" ./resumer list --source kimi-code --all --json > /tmp/kimi-arch.json
summ /tmp/kimi-arch.json
```

- Expected: `4 sessions`; `arch0001-1111-7000-8000-000000000009` absent.
- Passes. Before the fix this listed `5 sessions` with the archived session
  included (D1).
- Picker check: `RESUMER_KIMI_HOME="$R/kimi-arch" ./resumer --source kimi-code`
  must not show "Archived Kimi Session".

**A2 — OpenCode: no session is resurrected from the legacy JSON store (defects D2a, D2b)**

The SQLite filters are correct (`parent_id` set → child, `time_archived` set →
archived). The problem was the merge with the pre-1.1 JSON store: a leftover JSON
file for a session SQLite skips was appended as if it were live. The fix is the
source-of-truth policy described in 1.7: the JSON store is not read at all when a
database exists.

```bash
RESUMER_OPENCODE_DATA="$R/oc-arch" ./resumer list --source opencode --all --json > /tmp/oc-arch.json
summ /tmp/oc-arch.json
```

- Expected: `3 sessions`; `ses_bbbb…` absent.
- Passes. Before the fix this listed `5 sessions`: `ses_bbbb…` came back from the
  JSON copy, and so did the JSON-only `ses_jjjj…` (D2a). The 3 that remain are
  the database roots that are neither archived nor children.

**A2b — a child session in the legacy JSON store is still skipped (regression guard)**

```bash
RESUMER_OPENCODE_DATA="$R/oc-child" ./resumer list --source opencode --all --json > /tmp/oc-child.json
summ /tmp/oc-child.json
```

- Expected: `3 sessions`; `ses_cccc…` absent, because the JSON record names its
  parent. Passes, and must keep passing alongside the D2a/D2b fix.

**A3 — providers with no archive concept**

CodeBuddy exposes no archived-session state that resumer can read, so there is
nothing to exclude. Confirm by inspecting the store you
test, mark `N-A`, and say what you found. Do not invent an archive flag.

### 1.4 B — deleted sessions

A deleted session must disappear completely. Resumer discovers sessions by
enumerating files and directories, so a deleted session is normally just gone;
the risk is a stale index or a second copy of the data bringing it back.

**B2 — CodeBuddy: delete the session files**

```bash
RESUMER_CODEBUDDY_HOME="$R/cb-del" ./resumer list --source codebuddy --all --json > /tmp/cb-del.json
summ /tmp/cb-del.json
```

- Expected: `0 sessions`. Passes.

**B4 — Kimi Code: delete the session directory, keep the index entry**

```bash
RESUMER_KIMI_HOME="$R/kimi-del" ./resumer list --source kimi-code --all --json > /tmp/kimi-del.json
summ /tmp/kimi-del.json
```

- Expected: `3 sessions`; `dddd0002-2222-7000-8000-000000000002` absent although
  its `session_index.jsonl` entry is still present. Passes.

**B5 — OpenCode: delete the SQLite row, leave a JSON copy (defect D2b, fixed)**

```bash
RESUMER_OPENCODE_DATA="$R/oc-del" ./resumer list --source opencode --all --json > /tmp/oc-del.json
summ /tmp/oc-del.json
```

- Expected: `2 sessions`; `ses_aaaa…` absent.
- Passes: with `opencode.db` present the JSON store is never read, so neither an
  archived nor a deleted session can come back from a leftover file (D2b). See
  1.7 for why the JSON store is not merged. The store started from 3 visible
  database roots, so deleting one leaves 2 — the JSON copy must not add a third.
- Note for the report: recording the IDs SQLite scanned cannot fix this case,
  because a deleted row is never scanned at all.

### 1.5 C — sub-agent and sidechain records (related)

Sub-agent traffic must not be counted as the main session's prompts or assistant
turns. The CodeBuddy provider skips `isSidechain` records entirely (defect D3,
fixed); the case below pins that behavior.

**C2 — CodeBuddy**

```bash
RESUMER_CODEBUDDY_HOME="$R/cb-side" ./resumer list --source codebuddy --all --json > /tmp/cb-side.json
summ /tmp/cb-side.json
```

- Expected: `2 sessions`, and for `cb111111-1111-4111-8111-111111111111`
  `prompts=2` and `asst_count=1` — pinned by
  `internal/provider/codebuddy.TestFixtureParsing`.
- Known result: passes. Before the `isSidechain` fix this reported
  `prompts=3`, `asst_count=2` (D3).
- If your CodeBuddy version never writes `isSidechain`, mark `N-A` and say so.
  That is useful information, not a pass.

### 1.6 D — deleted working directory (the session still exists)

A different failure mode: the session is present, but the directory it was
created in is gone. Resumer labels the row `(deleted)`, refuses to resume, and
prints the command that unblocks it. Automated coverage: `internal/cwd`
(`TestResolve`, `TestResolveRecoversStaleStoredCwd`,
`TestResolveUnknownCwdIsNotRefused`), `internal/render`
(`TestRenderersFlagMissingWorkingDirectory`), `tests/integration`
(`TestUnifiedRender`, `TestSelectRefusesWhenWorkingDirectoryIsGone`).

Manual check with the kimi fixture session whose cwd
(`/tmp/resumer-fixtures/kimi-four`) is deliberately never created:

```bash
./resumer list --source kimi-code --all     # the row is labeled "(deleted)"
./resumer --source kimi-code                # select that session, press Enter
```

- Expected: the list labels the row `(deleted)`; on Enter resumer prints
  `error: cannot resume [kimi-code] <id> — its working directory has been
  deleted:`, the path, and `mkdir -p "<path>"`, then exits with status 3 without
  launching the agent CLI.
- After `mkdir -p /tmp/resumer-fixtures/kimi-four` the label disappears and
  Enter resumes normally.
- A session with no recorded cwd keeps showing `(unknown)` and is **not**
  refused — that is section 3.2.

### 1.7 Known defects and open questions

| ID | provider | symptom | status |
|---|---|---|---|
| D1 | kimi-code | archived sessions are listed and offered for resume | fixed — `parseSessionDir` returns `nil` for a session whose `state.json` says `"archived": true`, so `ListSessions` and `LoadDetail` both skip it |
| D2a | opencode | archived/child sessions resurrect from the legacy JSON store | fixed — with a database present the JSON store is not read at all (see the policy below) |
| D2b | opencode | a session whose SQLite row was deleted resurrects from a JSON copy | fixed — same policy: SQLite is the only source of truth when it exists |
| D3 | codebuddy | `isSidechain` records count as the main session's prompts/turns | fixed — the provider skips `isSidechain` records entirely |

All four fixes carry regression tests that fail without them:
`internal/provider/kimi.TestArchivedSessionNotListed`,
`internal/provider/opencode.TestLegacyJSONIgnoredWhenDatabaseExists`,
`internal/provider/opencode.TestArchivedSessionNotResurrectedFromJSON`, and
`internal/provider/codebuddy.TestSidechainRecordsDoNotCount`.

**Policy behind D2a/D2b (cases A2, A2b, B5): the database is the only source of
truth when it exists.** Recording every ID SQLite scanned — the first fix
attempted for D2a — cannot cover D2b: a deleted row is never scanned, so a JSON
copy of it is indistinguishable from a legitimate JSON-only session using the
data alone. The pre-1.1 JSON store is therefore *not* merged with the database;
it is a fallback for installs that predate the database (`listRaw`: if
`dbPath() != ""` → SQLite only, else → JSON only). That matches the agent:
OpenCode empties `storage/session/` when it migrates to SQLite and never
produces a JSON-only session beside a database, and its own listing query is
`WHERE time_archived IS NULL AND parent_id IS NULL`, so a JSON leftover beside a
database is by definition a session OpenCode no longer lists. Third-party
OpenCode readers keep the same rule (SQLite as the source of truth when the
session table exists; JSON only when there is no database). The JSON parser is
still covered by tests that build a store without `opencode.db`.

## 2. Condensed regression — previously verified behavior

These behaviors have been verified repeatedly and are pinned by automated tests.
Run `go test ./...` instead of re-doing them by hand; the manual column is a
one-minute sanity check only.

| behavior | automated coverage | manual check |
|---|---|---|
| merged list renders every active provider with badges | `tests/integration.TestUnifiedRender` | `./resumer list --all --no-group` shows `[cb] [kimi] [oc]` rows |
| sessions grouped by workspace; same directory merges across providers | `internal/workspace`, `internal/render.TestIndexGroupedMergesByDirectory`, `tests/integration.TestGroupedRender`, `internal/tui.TestSessionListGroupsByWorkspace` | `./resumer list --all` shows `── <dir>  ·  N sessions  ·  <providers>` headers (0.4) |
| kimi native workspace id (session bucket) + `workspaces.json` root | `internal/provider/kimi.TestFixtureParsing`, `TestISOTimestampsAndWorkDirParsed` | `./resumer list --source kimi-code --all --json` shows `workspace_id` |
| opencode `workspace_id`/`project_id`, `global` sentinel ignored | `internal/provider/opencode.TestSQLiteParsing`, `TestGlobalProjectHasNoWorkspaceID`, `TestJSONFallbackSession` | `./resumer list --source opencode --all --json` |
| deleted working directory labeled, resume refused, `mkdir -p` printed, exit 3 | `internal/cwd.*`, `internal/render.TestRenderersFlagMissingWorkingDirectory`, `tests/integration.TestSelectRefusesWhenWorkingDirectoryIsGone` | 1.6 |
| unknown cwd shows `(unknown)` and is not refused | `internal/cwd.TestResolveUnknownCwdIsNotRefused`, `internal/provider/kimi.TestSessionWithoutIndexEntryOrWire` | pick a kimi session with no recorded cwd |
| archived + child sessions skipped in SQLite | `internal/provider/opencode.TestArchivedAndChildSkipped` | A2 / A2b |
| archived kimi sessions skipped in `ListSessions` and `LoadDetail` | `internal/provider/kimi.TestArchivedSessionNotListed` | A1 |
| kimi unused (empty) sessions skipped — no title, no prompts, lifecycle-only wire | `internal/provider/kimi.TestUnusedSessionNotListed` | open kimi in a directory, exit without prompting, then `./resumer list --source kimi-code --all` |
| kimi ISO strings in `createdAt`/`updatedAt` still yield timestamps and cwd | `internal/provider/kimi.TestISOTimestampsAndWorkDirParsed` | 3.3 |
| legacy JSON ignored when `opencode.db` exists; used only without one | `internal/provider/opencode.TestLegacyJSONIgnoredWhenDatabaseExists`, `internal/provider/opencode.TestJSONFallbackSession` | A2 / A2b / B5 |
| provider enable/disable; a disabled provider is not scanned | `internal/provider.TestActiveFiltersDisabledProviders`, `internal/cli.TestProviderCmdOffListOn` | `./resumer provider off kimi-code`, then `./resumer list --all` |
| date / project / limit filters | `internal/provider/*.TestDateFilter`, `TestProjectFilter`, `TestFilters` | `./resumer list --days 7` |
| picker sort toggle and source cycling | `tests/integration.TestSortToggleAndSourceCycle` | press the sort key, then cycle source |
| resume exec per provider | `tests/integration.Test{CodeBuddy,Kimi,OpenCode}SelectExec` | select one session per provider |
| UI text is English, including the `(deleted)` label | `internal/textutil.DirDeletedLabel` and the render tests | eyeball the list |

Smoke test against real data — one command per provider, then compare with the
agent's own session picker:

```bash
./resumer list --source codebuddy  --all
./resumer list --source kimi-code  --all
./resumer list --source opencode   --all
```

These read the default roots from 0.2, so they need real local agent data; with
none installed they stop with the availability error from 0.3. Point the
overrides at a copy of a fixture instead if you only want to exercise the
rendering.

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

### 3.3 Kimi `state.json` timestamp forms (fixed)

kimi-code has written `createdAt`/`updatedAt` in two shapes: epoch-ms numbers in
current builds, ISO strings in older ones. The provider now accepts both in the
camelCase fields (`json.RawMessage` plus the existing `metaTS` normalization),
alongside the snake_case `created_at`/`updated_at` fallback it already had.
Before this, an ISO string in `createdAt` failed that one field and the session
lost its `state.json` timestamps, so a session whose wire stream carries no
`time` values had no timestamps at all and dropped out of `--days`/`--date`
filtering.

Regression fixture: `tests/fixtures/kimi-iso-home` — one session with ISO
camelCase timestamps, a recorded `workDir`, and no `session_index.jsonl` entry
(old kimi wrote none) — pinned by
`internal/provider/kimi.TestISOTimestampsAndWorkDirParsed`.

A tempting wrong diagnosis, recorded so it is not repeated: Go's
`json.Unmarshal` reports the type error for `createdAt` but **keeps decoding the
remaining fields**, so `title`, `workDir` and `lastPrompt` are *not* lost — only
the two timestamp fields are. (Verified with a standalone probe; the regression
test above still passes its title and cwd assertions against the pre-fix code.)
So a session that shows `(unknown)` is not explained by this bug: its
`state.json` has no `workDir`/`cwd` and no index entry, which is 3.2.

### 3.4 An OpenCode database whose table is not called `session`

The 1.7 policy reads only `session` from `opencode.db`. OpenCode 2.0.18 renamed
that table (`session_v2`), and resumer does not know the new name, so on such a
version the provider fails with

```
error: opencode db ~/.local/share/opencode/opencode.db: sqliteread: table not found
```

instead of listing anything (a `storage/` directory has to exist as well, or
`IsAvailable` declines first and you get the plain "not available" error). It
does **not** silently fall back to the JSON store — a database is only trusted
when it can be read. To confirm which name your install uses:

```bash
sqlite3 ~/.local/share/opencode/opencode.db ".tables"
./resumer list --source opencode --all
```

If the table is `session_v2`, the fix is to accept both names (and map the
renamed columns) in `readSQLite`. Deliberately not applied yet — no fixture or
installed version has been checked against it, and guessing a column layout is
worse than the current error.

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
run "A2  opencode archived  (expect 3, no ses_bbbb)" \
    RESUMER_OPENCODE_DATA="$R/oc-arch" ./resumer list --source opencode --all --json
run "A2b opencode child     (expect 3, no ses_cccc)" \
    RESUMER_OPENCODE_DATA="$R/oc-child" ./resumer list --source opencode --all --json
run "B5  opencode deleted   (expect 2, no ses_aaaa)" \
    RESUMER_OPENCODE_DATA="$R/oc-del" ./resumer list --source opencode --all --json
run "B2  codebuddy deleted  (expect 0)" \
    RESUMER_CODEBUDDY_HOME="$R/cb-del" ./resumer list --source codebuddy --all --json
run "C2  codebuddy sidechain (cb111111 must stay prompts=2 asst=1)" \
    RESUMER_CODEBUDDY_HOME="$R/cb-side" ./resumer list --source codebuddy --all --json
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
| A2 opencode archived leftover | 3, `ses_bbbb…` absent | | | |
| A2b opencode child leftover | 3, `ses_cccc…` absent | | | |
| A3 no-archive providers | nothing to exclude | | | |
| B2 codebuddy deleted | 0 | | | |
| B4 kimi deleted dir | 3, `dddd0002…002` absent | | | |
| B5 opencode deleted row | 2, `ses_aaaa…` absent | | | |
| C2 codebuddy sidechain | `cb111111…` stays 2 / 1 | | | |
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
