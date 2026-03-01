#!/usr/bin/env bash
# OSIRIS JSON validate-golden.sh - Validates all golden files against @osirisjson/core via the canonical CLI.
#
# Usage:
#   ./scripts/validate-golden.sh [--profile strict|default|basic] [pattern]
#
# Examples:
#   ./scripts/validate-golden.sh                              # validate all golden files at strict
#   ./scripts/validate-golden.sh --profile default            # validate at default profile
#   ./scripts/validate-golden.sh networking/cisco/nxos        # validate only NX-OS golden files
#
# Requirements:
#   Node.js (for npx) and @osirisjson/cli (fetched automatically by npx if not installed).
#
# Exit codes:
#   0 - all golden files pass validation
#   1 - at least one golden file has validation errors
#   2 - operational error (no golden files found, npx not available, etc.)

set -euo pipefail

PROFILE="strict"
PATTERN=""

# Parse the arguments
while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)
      PROFILE="$2"
      shift 2
      ;;
    --help|-h)
      head -20 "$0" | grep '^#' | sed 's/^# \?//'
      exit 0
      ;;
    *)
      PATTERN="$1"
      shift
      ;;
  esac
done

# Check for npx
if ! command -v npx &>/dev/null; then
  echo "error: npx not found. Install Node.js to use this script." >&2
  exit 2
fi

# Find golden files
SEARCH_DIR="${PATTERN:-.}"
GOLDEN_FILES=$(find "$SEARCH_DIR" -name 'golden.json' -type f 2>/dev/null | sort)

if [[ -z "$GOLDEN_FILES" ]]; then
  echo "error: no golden.json files found in ${SEARCH_DIR}" >&2
  exit 2
fi

COUNT=$(echo "$GOLDEN_FILES" | wc -l)
echo "Validating ${COUNT} golden file(s) at profile=${PROFILE}..."
echo ""

FAILED=0
for f in $GOLDEN_FILES; do
  if npx @osirisjson/cli validate --profile "$PROFILE" "$f" 2>&1; then
    echo "  PASS  $f"
  else
    echo "  FAIL  $f"
    FAILED=1
  fi
done

echo ""
if [[ $FAILED -eq 0 ]]; then
  echo "All golden files passed validation."
  exit 0
else
  echo "Some golden files failed validation." >&2
  exit 1
fi
