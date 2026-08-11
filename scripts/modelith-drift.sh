#!/usr/bin/env bash
# modelith-drift — detect domain concepts introduced in code without model updates.
#
# Extracts canonical names from the domain model YAML and checks the current
# diff for new exported Go identifiers that look like domain concepts but have
# no corresponding model entry. Non-blocking warnings — renames of modeled
# concepts would be caught by the compiler, not this script.
set -euo pipefail

MODEL="${1:-docs/domain-model/tell-me-go.modelith.yaml}"
BASE="${2:-origin/main}"

if [ ! -f "$MODEL" ]; then
  echo "modelith-drift: model file not found: $MODEL" >&2
  exit 0
fi

# ── 1. Extract canonical names from the model ──────────────────────────────

# Entity names (PascalCase keys under entities:)
entities=$(grep -E '^  [A-Z][A-Za-z]+:$' "$MODEL" | sed 's/^  //;s/:$//' | sort -u)

# Glossary terms
glossary=$(sed -n '/^glossary:/,/^enums:/p' "$MODEL" \
  | grep -E '^  [A-Za-z]+:' | sed -E 's/^  ([A-Za-z]+):.*$/\1/' | sort -u)

# Enum names
enums=$(sed -n '/^enums:/,/^entities:/p' "$MODEL" \
  | grep -E '^  [A-Z][A-Za-z]+:$' | sed 's/^  //;s/:$//' | sort -u)

# Enum values (under each enum)
enum_values=$(sed -n '/^enums:/,/^entities:/p' "$MODEL" \
  | grep -E '^      - name: ' | sed 's/.*name: //' | sort -u)

# Invariant ids (kebab-case)
invariants=$(grep -E '^      - id: ' "$MODEL" | sed 's/.*id: //' | sort -u)

# Combine all known names
all_names=$(printf '%s\n%s\n%s\n%s\n%s' \
  "$entities" "$glossary" "$enums" "$enum_values" "$invariants" | sort -u)

# ── 2. Find new exported identifiers in the diff ───────────────────────────

# Get changed files against base
changed=$(git diff --name-only "$BASE" -- '*.go' '!*_test.go' '!vendor/*' 2>/dev/null || true)

if [ -z "$changed" ]; then
  exit 0
fi

# Extract new exported type/interface/const/var names from added lines.
# We look for:
#   type Foo struct / interface
#   const Foo = ...
#   var Foo ...
# Exclude test files, vendor, and generated files.
new_exports=$(git diff "$BASE" -- $changed \
  | grep -E '^\+type [A-Z][A-Za-z0-9_]+ (struct|interface)' \
  | sed 's/^+type //;s/ \(struct\|interface\).*//' \
  | sort -u || true)

# Also catch exported const/var blocks that define domain-like names
new_consts=$(git diff "$BASE" -- $changed \
  | grep -E '^\+[[:space:]]*[A-Z][A-Za-z0-9_]+ =' \
  | sed 's/^+[[:space:]]*//;s/ =.*//' \
  | sort -u || true)

new_exports=$(printf '%s\n%s' "$new_exports" "$new_consts" | sort -u | grep -v '^$' || true)

# ── 3. Cross-reference ─────────────────────────────────────────────────────

warnings=0
warned_names=""
for name in $new_exports; do
  # Skip common non-domain names (infrastructure, stdlib-like)
  case "$name" in
    Config|Option|Builder|Error|Request|Response|Result|Handler|Server|Client|Mock*|Test*) continue ;;
  esac

  # Skip if name is in the model or is a common suffix
  if echo "$all_names" | grep -qxF "$name"; then
    continue
  fi

  # Skip if it looks like a suffix of a modeled name (e.g., EngineConfig)
  modeled=false
  for m in $entities; do
    case "$name" in
      *"$m"*) modeled=true; break ;;
    esac
  done
  if $modeled; then
    continue
  fi

  echo "  ⚠ new export '$name' has no corresponding model entry"
  warned_names="$warned_names $name"
  warnings=$((warnings + 1))
done

if [ "$warnings" -gt 0 ]; then
  echo "  → consider updating $MODEL if any listed exports are domain concepts"
  echo ""
  echo "$warnings warning(s) — model may be out of date"
fi

exit 0  # Never fail the build — advisory only
