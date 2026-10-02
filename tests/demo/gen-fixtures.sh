#!/bin/bash
# Generates synthetic demo sessions for the README GIF recording.
#
# Why not record against real agent data: the GIF is committed and public —
# real first prompts and project names must never leak into it.
# Timestamps are generated relative to "now" so the recording always looks
# fresh, regardless of when it is re-recorded.
#
# Output: tests/output/demo-fixtures/ (gitignored) + env.sh to source.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$REPO_ROOT/tests/output/demo-fixtures"
rm -rf "$OUT"
mkdir -p "$OUT/codebuddy/projects" "$OUT/kimi/sessions" "$OUT/opencode/storage/session" "$OUT/opencode/storage/message"

# ts_ago <minutes> — ISO-8601 UTC timestamp N minutes in the past (BSD/GNU date).
ts_ago() {
  if date -u -v-1M +%Y >/dev/null 2>&1; then
    date -u -v-"$1"M +%Y-%m-%dT%H:%M:%S.000Z
  else
    date -u -d "-$1 minutes" +%Y-%m-%dT%H:%M:%S.000Z
  fi
}

# ms_ago <minutes> — the same moment as epoch milliseconds.
ms_ago() {
  local iso
  iso="$(ts_ago "$1")"
  local secs
  secs="$(date -u -d "$iso" +%s 2>/dev/null || date -u -j -f "%Y-%m-%dT%H:%M:%S.000Z" "$iso" +%s)"
  echo $(( secs * 1000 ))
}

cb_session() { # bucket uuid cwd t0 t1 p1 p2 ttype title
  local bucket="$1" uuid="$2" cwd="$3" t0="$4" t1="$5" p1="$6" p2="$7" ttype="$8" title="$9"
  local dir="$OUT/codebuddy/projects/$bucket"
  mkdir -p "$dir"
  local f="$dir/$uuid.jsonl"
  {
    printf '{"type":"topic","topic":"%s","timestamp":"%s","sessionId":"%s"}\n' "$title" "$t0" "$uuid"
    printf '{"type":"message","role":"user","timestamp":"%s","sessionId":"%s","cwd":"%s","message":{"content":"%s"}}\n' "$t0" "$uuid" "$cwd" "$p1"
    printf '{"type":"message","role":"assistant","timestamp":"%s","sessionId":"%s","message":{"content":[{"type":"output_text","text":"on it"}]}}\n' "$t0" "$uuid"
    printf '{"type":"message","role":"user","timestamp":"%s","sessionId":"%s","cwd":"%s","message":{"content":"%s"}}\n' "$t1" "$uuid" "$cwd" "$p2"
    printf '{"type":"message","role":"assistant","timestamp":"%s","sessionId":"%s","message":{"content":[{"type":"output_text","text":"done"}]}}\n' "$t1" "$uuid"
    if [[ -n "$ttype" ]]; then
      printf '{"type":"%s","timestamp":"%s","%s":"%s"}\n' "$ttype" "$t1" \
        "$([[ "$ttype" == custom-title ]] && echo customTitle || echo aiTitle)" "$title"
    fi
  } > "$f"
}

kimi_session() { # bucket uuid cwd ms0 ms1 prompt
  local bucket="$1" uuid="$2" cwd="$3" ms0="$4" ms1="$5" p1="$6"
  local dir="$OUT/kimi/sessions/$bucket/session_$uuid"
  mkdir -p "$dir/agents/main"
  printf '{"id":"session_%s","version":2,"cwd":"%s","createdAt":%s,"updatedAt":%s,"archived":false}\n' \
    "$uuid" "$cwd" "$ms0" "$ms1" > "$dir/state.json"
  {
    printf '{"type":"metadata","protocol_version":"1.5","created_at":%s}\n' "$ms0"
    printf '{"type":"turn.prompt","time":%s,"input":"%s","origin":{"kind":"user"}}\n' "$ms0" "$p1"
    printf '{"type":"turn.ended","time":%s,"turnId":"t1","reason":"completed"}\n' "$ms1"
  } > "$dir/agents/main/wire.jsonl"
}

oc_session() { # projectID ses_uuid cwd ms0 ms1 title prompt
  local proj="$1" uuid="$2" cwd="$3" ms0="$4" ms1="$5" title="$6" p1="$7"
  local sdir="$OUT/opencode/storage/session/$proj"
  local mdir="$OUT/opencode/storage/message/$uuid"
  mkdir -p "$sdir" "$mdir"
  printf '{"id":"%s","projectID":"%s","directory":"%s","title":"%s","version":"1.18.33","time":{"created":%s,"updated":%s}}\n' \
    "$uuid" "$proj" "$cwd" "$title" "$ms0" "$ms1" > "$sdir/$uuid.json"
  printf '{"id":"msg_%s_1","role":"user","parts":[{"type":"text","text":"%s"}],"metadata":{"time":{"created":%s}}}\n' \
    "$uuid" "$p1" "$ms0" > "$mdir/msg_${uuid}_1.json"
  printf '{"id":"msg_%s_2","role":"assistant","parts":[{"type":"text","text":"done"}],"metadata":{"time":{"created":%s}}}\n' \
    "$uuid" "$ms1" > "$mdir/msg_${uuid}_2.json"
}

cb_session "-Users-demo-code-api-server" "11111111-aaaa-4000-8000-000000000001" \
  "/Users/demo/code/api-server" "$(ts_ago 95)" "$(ts_ago 10)" \
  "Add rate limiting to the public API endpoints" \
  "Looks good - now add tests for the burst case" \
  "custom-title" "Rate limiting middleware"

cb_session "-Users-demo-code-webapp" "22222222-bbbb-4000-8000-000000000002" \
  "/Users/demo/code/webapp" "$(ts_ago 180)" "$(ts_ago 62)" \
  "결제 모듈 에러 핸들링 정리해줘 - 중복 try/catch가 너무 많아" \
  "좋아, PR 올리기 전에 테스트도 추가해줘" \
  "ai-title" "Payment error handling cleanup"

cb_session "-Users-demo-dotfiles" "33333333-cccc-4000-8000-000000000003" \
  "/Users/demo/dotfiles" "$(ts_ago 1500)" "$(ts_ago 1380)" \
  "Migrate my zsh config to a modular layout" \
  "Also split the aliases into their own file" \
  "" ""

kimi_session "wd-api-server_0a1b2c3d4e5f" "44444444-dddd-4000-8000-000000000004" \
  "/Users/demo/code/api-server" "$(ms_ago 40)" "$(ms_ago 25)" \
  "Write integration tests for the auth flow"

kimi_session "wd-data-pipeline_1b2c3d4e5f6a" "55555555-eeee-4000-8000-000000000005" \
  "/Users/demo/code/data-pipeline" "$(ms_ago 260)" "$(ms_ago 190)" \
  "Profile the ETL job and find the bottleneck"

oc_session "proj-api-server" "ses_66666666-ffff-4000-8000-000000000006" \
  "/Users/demo/code/api-server" "$(ms_ago 300)" "$(ms_ago 200)" \
  "API server docs" \
  "Document the public API endpoints"

cat > "$OUT/env.sh" <<EOF
export RESUMER_CODEBUDDY_HOME="$OUT/codebuddy"
export RESUMER_KIMI_HOME="$OUT/kimi"
export RESUMER_KIMI_BIN=kimi
export RESUMER_OPENCODE_DATA="$OUT/opencode"
export RESUMER_OPENCODE_BIN=opencode
export PATH="$REPO_ROOT/tests/mock-bin:\$PATH"
EOF

echo "demo fixtures: $OUT"
