<!--
Copyright (c) 2026 gosharplite@gmail.com
SPDX-License-Identifier: MIT
-->

# ADR-035: Per-Request Goroutine Model for Capturer Reads

## Status
Accepted

## Decision Date
2026-05-14

## Context

The capturer (`internal/ui/capture.go`) previously used a persistent worker
goroutine to serialize reads on `c.reader` (a `*bufio.Reader`, which is not
concurrency-safe). The worker received read requests via an unbuffered
`requestChan`, spawned a nested read goroutine, and raced the caller's context
against the blocking read syscall.

**Problem:** When the caller's context cancelled during a read, the worker's
context branch executed `return`, exiting the entire worker goroutine. This
was intentional — the nested read goroutine leaked, holding `c.reader`, and
returning prevented a second request from racing on the corrupted reader.

However, the worker exit was permanent:

1. `c.requestChan` was never closed or nil'd in this path.
2. Any subsequent `sendRequest` call passed the nil-check (channel still
   non-nil), unblocked on `c.requestChan <- req`, and blocked forever since
   no receiver existed.
3. The caller eventually hit its own context timeout, returning a misleading
   `context.DeadlineExceeded` instead of a clear `errCapturerClosed`.

A single Ctrl+C during one prompt silently disabled the capturer for the
rest of the process lifetime (issue #385).

### Options Considered

**Option A — Fast-fail subsequent requests:** On worker exit, atomically nil
`c.requestChan` under `c.readerMu` so subsequent `sendRequest` calls return
`errCapturerClosed`. Rejected: unresolvable race between nil'ing the channel
and an in-flight channel send.

**Option B — Per-request goroutine model:** Drop the persistent worker.
Each `sendRequest` spawns its own goroutine that holds `readerMu` for the
duration of the blocking read. Context cancellation returns immediately; the
read goroutine drains naturally. **Accepted.**

**Option C — Resilient worker:** Keep the worker alive after cancellation by
detaching the leaked read goroutine through a "drain" channel. Rejected: adds
a third concurrency primitive (channel, mutex, drain channel) without benefit
over Option B.

## Decision

Replace the persistent worker goroutine + `requestChan` architecture with a
**per-request goroutine model**:

```
sendRequest(ctx, req):
  1. c.closed.Load() → return errCapturerClosed if closed (atomic, no lock)
  2. reader := c.reader  (pointer is immutable, no lock needed)
  3. go func():
       c.readerMu.Lock()
       defer c.readerMu.Unlock()
       if c.needsReset.Swap(false): reader.Reset(c.Stdin)
       perform blocking read on reader
       readDone <- result
  4. select { case <-readDone: return result; case <-ctx.Done(): }
     → c.needsReset.Store(true); return ctx.Err()
```

### Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| `readerMu` serializes access to `c.reader` | `bufio.Reader` is not concurrency-safe; only one goroutine may read at a time |
| `closed` and `needsReset` are `atomic.Bool` | Cancellation path cannot acquire `readerMu` (held by draining goroutine); atomics prevent data races and deadlocks |
| `Close` does not acquire `readerMu` | `atomic.Bool` idempotency check avoids blocking on an in-flight read goroutine; `Stdin.Close()` unblocks the syscall |
| `bufio.Reader.Reset()` after cancellation | Cancelled `ReadString` may leave partial data in the internal buffer; resetting discards it for the next read |
| Self-healing via natural drain | The leaked goroutine from a cancelled read eventually completes its syscall, releases `readerMu`, and exits; the next read proceeds normally |

## Consequences

### Positive
- **Self-healing.** After a context-cancelled read, the next read on the same
  capturer succeeds once the drained goroutine's syscall completes.
- **Deterministic close semantics.** `Close` returns immediately regardless of
  in-flight reads. Idempotent via `atomic.Bool`.
- **No persistent goroutine to manage.** No worker lifecycle, no channel teardown,
  no channel-close-during-send panics.
- **Zero data races.** Verified with `go test -race`. The two atomics eliminate
  the lock ordering problems that plagued the worker model.
- **Simpler concurrency model.** Two primitives: one mutex (serialization) and
  two atomics (state flags). Down from: one mutex, one unbuffered channel, one
  done channel, and a persistent goroutine.

### Negative
- **One goroutine per read.** Each `sendRequest` spawns a goroutine. Reads are
  serialized by `readerMu`, so at most 1 active + 0–1 draining goroutines exist
  at any time. Human-scale input rates make this negligible.
- **Cancelled read goroutine temporarily holds `readerMu`.** The next
  `sendRequest` blocks on `readerMu.Lock()` until the drained goroutine's
  syscall completes. In practice, this resolves when data arrives, EOF occurs,
  or `Close()` is called — typically sub-millisecond.

### Neutral
- **No API surface change.** All public methods (`ReadLine`, `ReadSingleKey`,
  `CapturePrompt`, `Confirm`, `Close`) retain identical signatures and return
  semantics.
- **`readRequest.resCh` and `readRequest.ctx` fields removed.** These were
  internal to the worker model; their removal is transparent to callers.

## References

- Issue #384 — "Ctrl+C during multi-line input leaks goroutine and prints
  unclean warning" (original bug)
- Issue #385 — "capture.go: worker exits permanently on first context
  cancellation" (this issue)
- `internal/ui/capture.go` — implementation
- `internal/ui/capture_test.go::TestCapturer_ReadAfterCancellation` —
  regression test
