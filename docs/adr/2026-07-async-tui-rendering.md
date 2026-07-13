# Async TUI Rendering for Large Responses

## Context and Problem Statement

The TUI Progress model (`internal/ui/tui/progress/model.go`) experiences UI thread starvation (freezing) when receiving large `ResponseEvent` payloads. The freeze manifests as stuck timers and an unresponsive interface requiring `Ctrl-C` to exit. 

Root Cause Diagnosis:
1. `handleResponseEvent` and `handleWindowSizeMsg` both execute `m.mdRender` (Glamour markdown rendering + Chroma syntax highlighting) **synchronously** inside Bubble Tea's `Update` loop.
2. Glamour rendering of large outputs (e.g., 8k+ tokens) is CPU-heavy.
3. This blocks the single-threaded `Update` loop, preventing processing of `spinnerTickMsg`, `tea.KeyMsg`, and `channelClosedMsg`, appearing as a deadlock.

Although there is no O(N^2) streaming issue (events are emitted once per turn), the sheer cost of rendering large markdown blocks the event bus consumer.

## Decision Drivers

1. **Non-blocking UI Thread:** The Bubble Tea `Update` loop must never be blocked by heavy computation.
2. **Event Delivery Guarantees:** Keeping the UI thread unblocked ensures `channelClosedMsg` and control-plane events drain rapidly, preventing upstream publisher timeouts (Fix #1244).
3. **Thread Safety:** Existing `makeMarkdownRenderer` cache is not goroutine-safe if moved to an async command.
4. **Stale State Protection:** Async render results must not overwrite newer states caused by terminal resizes or new turns.

## Considered Options

1. **Synchronous Rendering (Current):** Blocks UI. Unacceptable.
2. **Debounced Async Rendering:** Adds complex throttling machinery. Rejected because `ResponseEvent` is only published once per turn (no token streaming).
3. **Async Rendering via `tea.Cmd` with Generation Guards:** Dispatch a goroutine to render markdown, falling back to plaintext immediately, and dropping stale results.

## Decision Outcome

Chosen option: **Async Rendering via `tea.Cmd` with Generation Guards**.

### Implementation Details

1. **Plaintext Fallback:** On `ResponseEvent` or terminal resize, `m.responseText` is immediately set to the cheap `m.rawResponseText`. The UI displays unformatted text instantly.
2. **Async Command (`renderCmd`):** A `tea.Cmd` is spawned containing the width, the target text, and an incremented `renderGeneration` integer.
3. **Thread-Safe Rendering:** The command instantiates a *new* `glamour.TermRenderer` each time, avoiding data races on the previous unprotected cache in `makeMarkdownRenderer`.
4. **Generation Guard:** When `mdRenderCompleteMsg` returns to `Update`, the model asserts `msg.generation == m.renderGeneration`. If mismatched (due to an intervening resize or new turn), the rendered text is discarded.

### Mitigation of Technical Debt

*   Removes the unprotected state (`cachedRenderer`, `lastWidth`) from `renderer.go`.
*   Unblocks the UI thread, ensuring spinner animations (`spinnerTickMsg`) remain fluid even during massive LLM response processing.

## Consequences

*   **Positive:** The TUI will never freeze during large response handling. Spinner ticks and keyboard interrupts (`Ctrl-C`) remain instantly responsive.
*   **Positive:** Removes the theoretical race condition on the Glamour TermRenderer cache.
*   **Neutral:** Brief flash of unstyled plaintext before the markdown formatting pops in (acceptable degrade).