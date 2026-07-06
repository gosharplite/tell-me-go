#!/usr/bin/env bash
# modelith-layers — verify modeled domain entities live in internal/domain/.
#
# Extracts entity names from the domain model and checks that the primary
# type definition (struct/interface) for each entity lives in the domain layer.
# Flags entities whose canonical definition is in application, infrastructure,
# or tools — indicating the model and code disagree about what is domain.
set -euo pipefail

MODEL="${1:-docs/domain-model/tell-me-go.modelith.yaml}"

if [ ! -f "$MODEL" ]; then
  echo "modelith-layers: model file not found: $MODEL" >&2
  exit 0
fi

# ── 1. Extract entity names ────────────────────────────────────────────────

entities=$(grep -E '^  [A-Z][A-Za-z]+:$' "$MODEL" \
  | sed 's/^  //;s/:$//' \
  | grep -v '^ProviderType$\|^LLMError$\|^ToolCategory$')

# ── 2. For each entity, find its primary type definition ───────────────────

warnings=0
layer_ok=0

for entity in $entities; do
  # Search for the primary struct/interface definition
  def=$(grep -rn "type $entity struct\|type $entity interface" internal/ --include='*.go' 2>/dev/null | head -1 || true)

  if [ -z "$def" ]; then
    echo "  ? $entity — no struct/interface found"
    continue
  fi

  file=$(echo "$def" | cut -d: -f1)

  case "$file" in
    internal/domain/*)
      echo "  ✓ $entity — domain layer"
      layer_ok=$((layer_ok + 1))
      ;;
    *)
      pkg=$(echo "$file" | sed 's|/[^/]*\.go$||')
      echo "  ⚠ $entity — $pkg/ (not domain)"
      warnings=$((warnings + 1))
      ;;
  esac
done

echo "  $layer_ok entity definitions in domain layer"

if [ "$warnings" -gt 0 ]; then
  echo ""
  echo "$warnings warning(s) — entity definitions outside internal/domain/"
  echo "  → either move the type to domain, or reconsider its model classification"
fi

exit 0  # Advisory only
