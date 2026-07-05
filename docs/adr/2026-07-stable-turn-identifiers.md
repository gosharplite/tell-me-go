# ADR-047: Stable Turn Identifiers via UUID

- **Status:** Accepted
- **Date:** 2026-07
- **Author:** Architect (tell-me-go)

## Context

Conversation turns are currently identified by their positional index within the history slice (`turnIndex int`). The `SetPinned` API, the `manage_history` tool, and the TUI history browser all use these integer indices to reference specific turns.

This approach is fragile under summarization compaction. When the `summarize_history` tool compacts older turns — replacing N raw turns with a single summary turn pair — the indices of all remaining turns shift. Any pinned turn that survived compaction now occupies a different index. The result is twofold:

1. **Pin identity is lost on re-reference.** After compaction, the agent or TUI cannot reliably toggle the pin state of a previously pinned turn because the index has changed. The in-memory `Pinned` boolean survives on the `llm.Content` struct, but the mapping from the external identifier (the index the agent received) to the correct content entry is broken.

2. **Manual summarization ignores pinned status.** The `summarize_history` tool accepts a raw `turns` count from the LLM and passes it directly to `SummarizeRange`. It does not consult the `Pinned` field at all. The auto-summarization path (via `TokenGatekeeper`) already uses `contiguousUnpinnedSelector` to skip pinned turns, but the tool-driven path has no such guard. An LLM that calls `summarize_history(turns=5)` will bulldoze through pinned turns.

A third, related concern is the `manage_history` tool's `index: integer` parameter: the LLM sees only integer indices in its conversation history. After compaction, the index it remembers is stale.

## Decision

**Add stable UUID-based turn identifiers and update all pin-related APIs to use them.**

### 1. `llm.Content` already carries an `ID string` field

The `llm.Content` struct already defines `ID string` (`json:"id,omitempty"`) populated by `llm.NewID()` (UUIDv4) at creation time. This was originally included for deduplication; it becomes the stable turn identity.

### 2. Change `SetPinned` signature

```go
// Before
SetPinned(ctx context.Context, turnIndex int, pinned bool) error

// After
SetPinned(ctx context.Context, turnID string, pinned bool) error
```

The implementation locates the turn by scanning `Contents` for a matching `ID` field, toggles `Pinned` on both messages in the turn, and persists the metadata update via `store.UpdateMetadata`.

### 3. Interface ripple

The `SetPinned` method belongs to the `HistoryManager` interface (`ports` package or equivalent). All ~30 implementations (production `history.Manager`, mocks in `agenttest`, `cli`, `ui/tui`, `infrastructure/di`, and test stubs) will adopt the new signature.

### 4. Lazy migration on `Load()`

Legacy history files predating this ADR will have empty `ID` fields. On `Load()`, the `Manager` will scan all loaded `Content` entries and backfill any missing IDs with fresh UUIDv4 values, then persist the updated file. This is a one-time, transparent migration with no user action required.

### 5. `manage_history` tool keeps `index: integer` in JSON schema

The LLM-facing JSON schema retains `index: integer` for agent ergonomics (LLMs reason naturally about "the 3rd turn"). At call time, the tool handler resolves the index to a UUID:

1. Acquire read lock on `Contents`.
2. Map `index` → `Contents[offset + index*2].ID`.
3. Release lock, call `SetPinned(ctx, id, pinned)`.

This preserves the LLM's existing mental model while insulating it from index drift. The resolution is re-performed on every call, so a newly compacted session uses the latest index mapping.

## Consequences

### Positive

- **Pin identity survives compaction.** Because the UUID is stable across the lifetime of a `Content` entry, pin toggles always target the correct turn regardless of how many compaction rounds occur.
- **TUI toggle round-trips correctly.** The TUI history browser resolves indices to UUIDs the same way `manage_history` does. Pinning/unpinning a turn in the TUI, then summarizing, then toggling again targets the same turn.
- **Manual summarization gains pin awareness.** The `summarize_history` tool path can now use `contiguousUnpinnedSelector` (already implemented for auto-summarization) to skip pinned turns, or at minimum warn when pinned turns would be destroyed. This closes the gap between auto-summarization (which respects pins) and manual summarization (which currently does not).
- **Forward-compatible migration.** The lazy backfill on `Load()` means no user-visible migration step. Old sessions silently gain UUIDs on first load.

### Negative

- **`google/uuid` becomes a direct dependency.** The `llm.NewID()` function currently generates UUIDs; if it doesn't already depend on `google/uuid`, it will now. (This is a minor, well-known dependency with no transitive risk.)
- **~30 call sites need interface updates.** Every implementation and mock of `SetPinned` must change its signature. This is mechanical — the compiler enforces correctness — but it is a large diff.
- **ID scan on `SetPinned` is O(n).** Scanning `Contents` by UUID is linear; for typical history sizes (hundreds, not thousands of turns) this is negligible. If profiling reveals it as a bottleneck, an internal `map[string]int` index can be added to the `Manager` without changing the public API.
- **Legacy history files without IDs get modified on load.** The lazy migration writes back to disk. For read-only filesystems or archived histories, the migration will fail on save. The `Load()` path should gracefully degrade — IDs are assigned in memory even if persistence fails.

### Trade-off

- **`manage_history` index → UUID resolution is a leaky abstraction.** The LLM still thinks in indices. After a compaction round, the LLM's mental index may be stale — it might say "pin turn 3" when turn 3 has already been compacted away. In practice, the LLM typically pins turns *before* summarizing, and the TUI is the primary interface for post-hoc pin management. The resolution layer is a pragmatic bridge, not a perfect solution. A future ADR could introduce `manage_history` with explicit UUID parameters if agent ergonomics improve.

## References

- [ADR-006: History Log Compaction and Bounded Contexts](2026-01-history-log-compaction.md)
- [ADR-008: Bubble Tea Interactive History Browser](2026-02-bubble-tea-history-browser.md)
- [ADR-019: JSONL History Persistence](2026-04-jsonl-history-persistence.md)
