#!/usr/bin/env bash
# Phase 8 — CI guard against inline "No data" / "No rows" placeholders.
#
# Every list/table surface under app/(app)/console must use the
# <EmptyState> component rather than a raw text placeholder. This script
# greps for the two banned phrases and fails the build if any survive.
#
# Run from anywhere; the grep is scoped to the web app's console tree
# relative to this script's location.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SEARCH_DIR="$ROOT/app/(app)/console"

if [ ! -d "$SEARCH_DIR" ]; then
  echo "check-no-data-strings: scan root not found: $SEARCH_DIR" >&2
  exit 2
fi

# -E enables ERE so the alternation works in BSD and GNU grep.
# We deliberately match the visible string, including quote types, so a
# JSX attribute like title="No data" trips the guard.
PATTERN='(No data|No rows)'

# --include keeps the scan limited to source files. Excluding this script
# itself prevents the literal pattern in this file from self-tripping.
HITS=$(grep -RnE \
  --include="*.ts" --include="*.tsx" --include="*.js" --include="*.jsx" \
  "$PATTERN" \
  "$SEARCH_DIR" || true)

if [ -n "$HITS" ]; then
  echo "check-no-data-strings: banned placeholder strings found." >&2
  echo "$HITS" >&2
  echo >&2
  echo "Replace with <EmptyState /> from components/empty-state/EmptyState.tsx." >&2
  exit 1
fi

echo "check-no-data-strings: ok — no banned placeholders found."
