#!/usr/bin/env bash
# modelith-layers — verify modeled domain entities live in internal/domain/.
#
# Extracts entity names from the domain model and checks that the primary
# type definition (struct/interface) for each entity lives in the domain layer.
# Entities with documented layer exceptions (Turn in orchestrator, Context as
# package, etc.) are noted but not flagged as warnings.
set -euo pipefail

MODEL="${1:-docs/domain-model/tell-me-go.modelith.yaml}"

if [ ! -f "$MODEL" ]; then
  echo "modelith-layers: model file not found: $MODEL" >&2
  exit 0
fi

# ── Documented exceptions ──────────────────────────────────────────────────
# Entities where the model acknowledges the code's layer placement.
# See docs/domain-model/README.md for rationale on each.
#
# Format: Entity|Reason

exceptions() {
  cat <<'EOF'
Turn|hybrid domain+application — the orchestrator Turn carries runtime deps
Context|package of cooperating types, not a single struct (by design)
Provider|LLMProvider lives in config; model documents the concept
Memory|MemoryConfig lives in config; model documents the concept
MCPServer|MCPServerConfig lives in config; model documents the concept
Pricing|consolidated into domain/pricing types (ModelPricing, PricingData)
Tool|tool declarations in domain/tools package
History|persistence lives in infrastructure; model documents the domain view
EOF
}

# ── 1. Extract entity names ────────────────────────────────────────────────

entities=$(grep -E '^  [A-Z][A-Za-z]+:$' "$MODEL" \
  | sed 's/^  //;s/:$//' \
  | grep -v '^ProviderType$' | grep -v '^LLMError$' | grep -v '^ToolCategory$' | grep -v '^APIFamily$' | grep -v '^MemoryLearnTier$')

# ── 2. Build exception map ─────────────────────────────────────────────────

declare -A exception_map
while IFS='|' read -r entity reason; do
  exception_map["$entity"]="$reason"
done < <(exceptions)

# ── 3. For each entity, find its primary type definition ───────────────────

layer_ok=0
exception_ok=0
missing=0
warnings=0

for entity in $entities; do
  def=$(grep -rn -E "type $entity struct|type $entity interface" internal/ --include='*.go' --exclude='*_test.go' 2>/dev/null | head -1 || true)

  if [ -z "$def" ]; then
    if [ -n "${exception_map[$entity]:-}" ]; then
      echo "  - $entity — ${exception_map[$entity]}"
      exception_ok=$((exception_ok + 1))
    else
      echo "  ? $entity — no struct/interface found"
      missing=$((missing + 1))
    fi
    continue
  fi

  file=$(echo "$def" | cut -d: -f1)

  case "$file" in
    internal/domain/*)
      echo "  ✓ $entity — domain layer"
      layer_ok=$((layer_ok + 1))
      ;;
    *)
      if [ -n "${exception_map[$entity]:-}" ]; then
        pkg=$(echo "$file" | sed 's|/[^/]*\.go$||')
        echo "  - $entity — $pkg/ (documented: ${exception_map[$entity]})"
        exception_ok=$((exception_ok + 1))
      else
        pkg=$(echo "$file" | sed 's|/[^/]*\.go$||')
        echo "  ⚠ $entity — $pkg/ (not domain)"
        warnings=$((warnings + 1))
      fi
      ;;
  esac
done

echo "  $layer_ok in domain, $exception_ok documented exceptions, $missing missing"

if [ "$warnings" -gt 0 ]; then
  echo ""
  echo "$warnings unclassified warning(s) — run \`make modelith-check\` and review"
fi

exit 0
