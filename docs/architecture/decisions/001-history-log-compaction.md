# ADR 001: History Log Compaction and Bounded Contexts

## Status
Proposed

## Context
The system currently uses an append-only event sourcing model for the conversation history, where all context turns (user inputs, LLM responses, and tool executions) are appended to a persistent `history.jsonl` file via the Context Manager. Upon a cache miss, the system loads and parses an unbounded `O(N)` history slice into memory.

This approach suffers from a critical unbounded memory and disk edge case:
1. **In-Memory Bloat**: Loading an unbound history file linearly increases memory consumption, leading to an eventual `O(N²)` operational footprint on long-running sessions, causing Out-Of-Memory (OOM) crashes.
2. **Context Limits (LLM)**: Large histories exceed the maximum token context window allowed by LLM providers.
3. **Disk I/O Latency**: Unbounded disk reading increases cache-miss latency and negatively impacts user experience.

## Considered Options

### In-Memory State: Bounded Context Strategy
1. **Strict Sliding Window**: Keep only the last `K` messages in memory.
   * *Pros*: Simple to implement.
   * *Cons*: May drop important early context or system instructions.
2. **Token-Based Truncation**: Truncate oldest messages dynamically based on actual token limits, ensuring we never exceed the LLM's context size.
   * *Pros*: Maximizes context usage.
   * *Cons*: Requires an accurate token counter (or a heuristic).
3. **Summary-Based Sliding Window**: Keep a sliding window of recent messages, while summarizing the dropped older messages into a single compressed "context" node.
   * *Pros*: Retains long-term narrative meaning without raw token bloat.
   * *Cons*: Requires an explicit LLM call to perform summarization.

### Persistent Storage Layer: Log Compaction/Snapshotting
1. **Periodic Snapshotting (Raft-style)**: Periodically (e.g., every 50 turns), write a `snapshot.json` file representing the current summarized state and truncate the `history.jsonl` file.
   * *Pros*: Matches standard Event Sourcing / Raft implementations; drastically reduces I/O.
   * *Cons*: Complexity in managing atomic snapshot+truncate operations.
2. **File Rotation and Archiving**: Rotate `history.jsonl` into `history.1.jsonl` once it exceeds a specific size, only parsing the latest active file.
   * *Pros*: Easy to implement using standard logging techniques.
   * *Cons*: Harder to cleanly merge context across file boundaries.

## Decision

We will implement a hybrid **Summary-Based Sliding Window** for in-memory context and **Periodic Snapshotting** for persistent storage.

1. **In-Memory Bounded Context**: 
   We will introduce a configurable token-based sliding window. When the history exceeds the configured token threshold (e.g., 8,000 tokens), the system will trigger a background summarization task. The older turns will be replaced by a single `SystemMessage` containing the semantic summary, while keeping the most recent `N` turns verbatim.

2. **Persistent Log Compaction**:
   We will adopt a snapshotting model for `history.jsonl`.
   * When history reaches a threshold length, the system will save a `snapshot.json` representing the current summarized memory state.
   * The active `history.jsonl` will be truncated.
   * Older raw logs will be rotated to an immutable `history.archive.jsonl` for offline analytics and debugging, ensuring no raw data is permanently lost.

## Consequences

**Positive:**
* **Scalability**: Memory consumption and read latencies become bounded `O(1)`.
* **Cost Efficiency**: We avoid sending massive, redundant context windows to the LLM, reducing per-turn token costs.
* **Stability**: Eliminates the risk of OOM crashes on long-running persistent sessions.

**Negative:**
* **Complexity**: Introduces asynchronous summarization tasks and atomic disk operations to ensure consistency during snapshotting.
* **Information Loss**: LLM may lose precise verbatim quotes from older turns, relying solely on the fidelity of the generated summary.
