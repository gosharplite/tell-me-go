#!/usr/bin/env bash
# modelith-vocab — detect non-canonical terminology in docs.
#
# Scans docs/ and README.md for known non-canonical synonyms of domain
# model terms. Only includes synonyms that clearly indicate someone is
# reaching for a domain concept by the wrong name — not generic English
# usage ("tool execution", "llm provider").
#
# Advisory only. POSIX-only.
set -euo pipefail

w=0

check() {
  local synonym="$1" canonical="$2"
  local matches
  matches=$(grep -rin "$synonym" docs/ README.md 2>/dev/null \
    | grep -v "docs/domain-model/" \
    | grep -v "\`$canonical\`" \
    || true)
  if [ -n "$matches" ]; then
    echo "  \"$synonym\" → use \`$canonical\`:"
    echo "$matches" | while read -r line; do echo "    $line"; done
    echo ""
    return 1
  fi
  return 0
}

check "prompt buffer"   "Context"   || w=$((w + 1))
check "prompt-buffer"   "Context"   || w=$((w + 1))
check "authorized path" "SafePath"  || w=$((w + 1))
check "safe path"       "SafePath"  || w=$((w + 1))
check "cost table"      "Pricing"   || w=$((w + 1))
check "pricing table"   "Pricing"   || w=$((w + 1))
check "rate card"       "Pricing"   || w=$((w + 1))
check "injected skill"  "Skill"     || w=$((w + 1))
check "embedded skill"  "Skill"     || w=$((w + 1))

if [ "$w" -eq 0 ]; then
  echo "  ✓ all docs use canonical vocabulary"
fi
