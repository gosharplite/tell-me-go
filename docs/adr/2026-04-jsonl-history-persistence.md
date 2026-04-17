<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-019: JSONL History Persistence

## Status
Accepted

## Context
Originally, the session history was stored as a single JSON array. This necessitated an atomic rewrite of the entire file every time a new message or tool result was added. As the session grew, this operation became $O(N^2)$ in terms of total bytes written over the life of the session, leading to performance degradation and increasing the window of risk for data corruption if the process crashed mid-write.

Furthermore, updating specific message metadata (like pinning a turn to prevent summarization) required reading the entire history, modifying the object in memory, and writing it back out.

## Decision
We decided to switch to a **JSONL (JSON Lines)** format for history persistence, complemented by an **incremental patch system**.

1. **JSONL Format**: Each turn (user message + assistant response + tool outputs) is written as a single JSON line. Appending a new turn is now a constant-time $O(1)$ disk operation.
2. **Patch System**: Metadata updates (e.g., pinning/unpinning) are recorded as separate "patch" lines in the same file or a companion file.
3. **Reconstruction**: Upon loading, the history is reconstructed by reading the base JSONL records and applying all subsequent patches sequentially.

## Consequences
- **Improved Performance**: Writing to history is now instantaneous regardless of session length.
- **Resilience**: Append-only writes are significantly more resilient to power failures or process crashes. Even if the last line is partial, previous history remains valid.
- **Architectural Shift**: The `HistoryManager` now handles stream-based parsing and state reconciliation (applying patches to base turns).
- **Compaction Strategy**: History is naturally compacted during operations that trigger an atomic rewrite of the full file (e.g., `RollbackTurns` or `SetContents`). Since the in-memory state always reflects the reconciled history (base records + applied patches), any call to `Save` effectively merges outstanding patches into clean base records, ensuring the log remains efficient over time without requiring a dedicated manual compaction command.
