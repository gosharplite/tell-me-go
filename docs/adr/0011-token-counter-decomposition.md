# ADR-0011: Token Counter Decomposition

**Status:** Accepted
**Date:** 2026-01-24
**Author:** Architecture Review (Issue #866)

## Context

`internal/agent/session/context/token_counter.go` sits on the hottest path of every
LLM inference — `Count` is called before every tool execution and every summarization
check. Two functions exceeded the recommended complexity threshold for domain logic:
`Count` (9) and `estimateValueSizeInternal` (10).

## Decision

Decompose `Count` into three single-responsibility sub-functions:

| Function | Responsibility | Complexity |
|----------|---------------|------------|
| `countToolDeclarationOverhead` | Registry tool declaration token overhead | 4 |
| `countContentTokens` | Per-content token estimation with caching side-effect | 3 |
| `accumulatePartsChars` | Sum `estimatePartChars` over a `[]*llm.Part` | 2 |

`Count` now orchestrates these at complexity 2.

For `estimateValueSizeInternal`, extract the two recursive branches (`map` and `slice`)
into standalone `estimateMapValueSize` and `estimateSliceValueSize` functions, reducing
the type-switch to simple, non-recursive cases. The remaining complexity 9 is inherent
to the flat type-switch pattern (8 branches), which is exempt per project thresholds (≤15).

## Approach Rejected

**Interface-based polymorphism:** A `ValueEstimator` interface with implementations for
each type would introduce virtual dispatch overhead on the hot path. Go interfaces add
2–3× call cost and risk heap allocation.

**Map-driven dispatch:** A `map[reflect.Kind]func(...)` would allocate on every call,
defeating the 0 B/op invariant.

## Consequences

- **Positive:** `Count` complexity reduced from 9 → 2. Each sub-function is independently
  testable and has a single, documented purpose.
- **Positive:** Zero allocation regression across all benchmark scenarios (0 B/op, 0 allocs/op).
- **Neutral:** ~10% ns/op increase in trivial benchmarks (Empty, TextOnly) due to function-call
  depth. MixedParts (most realistic) improved by 2%.
- **Negative:** `estimateValueSizeInternal` remains at complexity 9 — the type-switch is
  inherently branching. Further decomposition would be cosmetic.

## Invariants Preserved

1. `Content.TokenCount` is only cached when `TransientParts` is empty
2. Registry tool declarations never decrease the total count
3. All 21 fuzz seeds pass (nil variants, circular maps, exotic types, overflow)
4. `maxEstimateDepth` recursion guard prevents stack overflow on circular references
