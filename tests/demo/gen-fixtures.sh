#!/bin/bash
# Generates synthetic demo sessions for the README GIF recording.
#
# Why not record against real ~/.claude/projects: the GIF is committed and
# public — real first prompts and project names must never leak into it.
# Timestamps are generated relative to "now" so the recording always looks
# fresh, regardless of when it is re-recorded.
#
# Output: tests/output/demo-fixtures/ (gitignored) + env.sh to source.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$REPO_ROOT/tests/output/demo-fixtures"
rm -rf "$OUT"
mkdir -p "$OUT/claude-code" "$OUT/codex"

# ts_ago <minutes> — ISO-8601 UTC timestamp N minutes in the past (BSD/GNU date).
ts_ago() {
  if date -u -v-1M +%Y >/dev/null 2>&1; then
    date -u -v-"$1"M +%Y-%m-%dT%H:%M:%S.000Z
  else
    date -u -d "-$1 minutes" +%Y-%m-%dT%H:%M:%S.000Z
  fi
}

cc_session() { # dir uuid cwd ts_start ts_end first_prompt second_prompt title_type title
  local dir="$1" uuid="$2" cwd="$3" t0="$4" t1="$5" p1="$6" p2="$7" ttype="$8" title="$9"
  mkdir -p "$OUT/claude-code/$dir"
  local f="$OUT/claude-code/$dir/$uuid.jsonl"
  {
    printf '{"type":"system","subtype":"init","cwd":"%s","timestamp":"%s"}\n' "$cwd" "$t0"
    printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"%s"}}\n' "$t0" "$p1"
    printf '{"type":"assistant","timestamp":"%s","message":{"role":"assistant","content":[{"type":"text","text":"on it"}]}}\n' "$t0"
    printf '{"type":"user","timestamp":"%s","message":{"role":"user","content":"%s"}}\n' "$t1" "$p2"
    printf '{"type":"assistant","timestamp":"%s","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}\n' "$t1"
    if [[ -n "$ttype" ]]; then
      printf '{"type":"%s","timestamp":"%s","%s":"%s"}\n' "$ttype" "$t1" \
        "$([[ "$ttype" == custom-title ]] && echo customTitle || echo aiTitle)" "$title"
    fi
  } > "$f"
}

codex_session() { # uuid cwd ts_start ts_end prompt
  local uuid="$1" cwd="$2" t0="$3" t1="$4" p1="$5"
  local day="${t0%%T*}"
  local dir="$OUT/codex/${day//-//}"
  mkdir -p "$dir"
  local f="$dir/rollout-${t0%%.*}-$uuid.jsonl"
  f="${f//:/-}"
  {
    printf '{"timestamp":"%s","type":"session_meta","payload":{"id":"%s","timestamp":"%s","cwd":"%s","originator":"codex-tui","source":"cli"}}\n' "$t0" "$uuid" "$t0" "$cwd"
    printf '{"timestamp":"%s","type":"event_msg","payload":{"type":"task_started"}}\n' "$t0"
    printf '{"timestamp":"%s","type":"event_msg","payload":{"type":"user_message","message":"%s","images":[]}}\n' "$t0" "$p1"
    printf '{"timestamp":"%s","type":"event_msg","payload":{"type":"task_complete"}}\n' "$t1"
  } > "$f"
}

cc_session "-Users-demo-code-api-server" "11111111-aaaa-4000-8000-000000000001" \
  "/Users/demo/code/api-server" "$(ts_ago 95)" "$(ts_ago 10)" \
  "Add rate limiting to the public API endpoints" \
  "Looks good - now add tests for the burst case" \
  "custom-title" "Rate limiting middleware"

cc_session "-Users-demo-code-webapp" "22222222-bbbb-4000-8000-000000000002" \
  "/Users/demo/code/webapp" "$(ts_ago 180)" "$(ts_ago 62)" \
  "결제 모듈 에러 핸들링 정리해줘 - 중복 try/catch가 너무 많아" \
  "좋아, PR 올리기 전에 테스트도 추가해줘" \
  "ai-title" "Payment error handling cleanup"

cc_session "-Users-demo-dotfiles" "33333333-cccc-4000-8000-000000000003" \
  "/Users/demo/dotfiles" "$(ts_ago 1500)" "$(ts_ago 1380)" \
  "Migrate my zsh config to a modular layout" \
  "Also split the aliases into their own file" \
  "" ""

codex_session "019e1111-aaaa-7000-8000-000000000011" "/Users/demo/code/api-server" \
  "$(ts_ago 40)" "$(ts_ago 25)" \
  "Write integration tests for the auth flow"

codex_session "019e2222-bbbb-7000-8000-000000000022" "/Users/demo/code/data-pipeline" \
  "$(ts_ago 260)" "$(ts_ago 190)" \
  "Profile the ETL job and find the bottleneck"

cat > "$OUT/codex/session_index.jsonl" <<EOF
{"id":"019e1111-aaaa-7000-8000-000000000011","thread_name":"Auth flow tests","updated_at":"$(ts_ago 25)"}
{"id":"019e2222-bbbb-7000-8000-000000000022","thread_name":"ETL bottleneck hunt","updated_at":"$(ts_ago 190)"}
EOF

cat > "$OUT/env.sh" <<EOF
export RESUMER_CLAUDE_PROJECT_ROOT="$OUT/claude-code"
export RESUMER_CODEX_SESSION_ROOT="$OUT/codex"
export RESUMER_CODEX_INDEX_FILE="$OUT/codex/session_index.jsonl"
export RESUMER_CODEX_BIN=codex
export PATH="$REPO_ROOT/tests/mock-bin:\$PATH"
EOF

echo "demo fixtures: $OUT"
