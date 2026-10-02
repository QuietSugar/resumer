#!/bin/bash
# resumer QA orchestrator.
# Runs vet + the full Go test suite (unit + PTY-driven TUI integration),
# then optionally produces the VHS demo GIF.
#
# Usage:
#   ./tests/run-qa.sh              # tests + VHS demo
#   ./tests/run-qa.sh --no-vhs     # skip VHS demo
set -u

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUTPUT_DIR="$REPO_ROOT/tests/output"
TAPES=(
  "$REPO_ROOT/tests/resumer-demo.tape"
)
mkdir -p "$OUTPUT_DIR"

RUN_VHS=1
while (( $# > 0 )); do
  case "$1" in
    --no-vhs) RUN_VHS=0 ;;
    -h|--help) sed -n '2,8p' "$0"; exit 0 ;;
    *) echo "unknown arg: $1"; exit 2 ;;
  esac
  shift
done

# --- preflight ---
if ! command -v go >/dev/null; then
  echo "error: missing dependency: go (https://go.dev/dl/ or brew install go)"
  exit 2
fi
if [[ ! -x "$REPO_ROOT/tests/mock-bin/codebuddy" ]]; then
  echo "error: missing executable: $REPO_ROOT/tests/mock-bin/codebuddy"
  exit 2
fi
if (( RUN_VHS )) && ! command -v vhs >/dev/null; then
  echo "warn: vhs not installed; skipping GIF (use --no-vhs to silence)"
  RUN_VHS=0
fi

fail=0

echo "════════════════════════════════════════════════════════"
echo " resumer QA — go vet + go test (unit + TUI integration)"
echo "════════════════════════════════════════════════════════"

cd "$REPO_ROOT"

echo ""
echo "[1/2] go vet ./..."
echo "────────────────────────────────────────────────────────"
if go vet ./...; then
  echo "  ✓ vet clean"
else
  fail=1
fi

echo ""
echo "[2/2] go test ./..."
echo "────────────────────────────────────────────────────────"
if go test ./...; then
  echo "  ✓ all tests pass"
else
  fail=1
fi

# --- VHS demo ---
if (( RUN_VHS )); then
  for tape in "${TAPES[@]}"; do
    [[ -f "$tape" ]] || { echo "[VHS] skip (tape missing): $tape"; continue; }
    echo ""
    echo "[VHS] generating GIF from $(basename "$tape")"
    echo "────────────────────────────────────────────────────────"
    gif_rel="$(grep -m1 '^Output' "$tape" | awk '{print $2}' | tr -d '"')"
    gif_abs="$REPO_ROOT/$gif_rel"
    if vhs "$tape" 2>&1 | tail -5; then
      if [[ -f "$gif_abs" ]]; then
        size=$(du -h "$gif_abs" | awk '{print $1}')
        echo "  ✓ $(basename "$gif_abs") generated ($size)"
      else
        echo "  ✗ $(basename "$gif_abs") not found after vhs run"
        fail=1
      fi
    else
      echo "  ✗ vhs failed"
      fail=1
    fi
  done
fi

echo ""
echo "════════════════════════════════════════════════════════"
if (( fail == 0 )); then
  echo " QA PASS"
else
  echo " QA FAIL"
fi
echo "════════════════════════════════════════════════════════"
exit $fail
