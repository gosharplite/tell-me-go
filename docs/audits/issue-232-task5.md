## Task 5 Findings: Final Cross-Cutting Check

### Exhaustive HandleEvent Search

```
$ grep -rn 'HandleEvent' --include='*.go' | grep -v '_test.go' | grep -v 'internal/agent/session/ui/bridge.go' | grep -v 'internal/agent/session/session_manager.go'

./internal/agent/agenttest/helpers.go:241:  mockTurnsLogger.HandleEvent    (test mock — not affected)
./internal/infrastructure/logging/async_turns_logger.go:111: asyncTurnsLogger.HandleEvent   (TurnsLogger impl — not affected)
./internal/domain/ports/logger.go:29:  TurnsLogger interface              (separate contract — not affected)
./internal/domain/ports/logger.go:38:  NoOpTurnsLogger.HandleEvent       (no-op — not affected)
```

**Result: Zero additional `Bridge.HandleEvent` callers found.** The four matches above are all `TurnsLogger.HandleEvent` — a completely separate interface:

| Interface | Signature | Returns | Affected by PR #230? |
|-----------|-----------|---------|---------------------|
| `Bridge.HandleEvent` | `func(ctx context.Context, e events.Event) error` | `error` | ✅ Yes — `enqueueNonCritical` changed |
| `TurnsLogger.HandleEvent` | `func(ctx context.Context, e events.Event)` | (nothing) | ❌ No — fire-and-forget, no enqueue |

### pkg/ and cmd/ Verification

```
$ grep -rn 'enqueueNonCritical\|enqueueCritical\|enqueueEvent\|HandleEvent\|Bridge\.' pkg/ cmd/ --include='*.go'
(no matches)
```

**Result: Zero references.** The bridge and its enqueue methods are not exposed through the public API (`pkg/`) or CLI entry points (`cmd/`). The change is fully encapsulated within `internal/`.

### All Call Sites — Final Classification

| Layer | Call Site | Location | Classification |
|-------|-----------|----------|----------------|
| **Subscriber** | Bridge subscriber lambda | `session_manager.go:295` | ⚠️ Behavioral drift — `context.Canceled` now returned where previously silently dropped |
| **Publisher ×8** | All event producers | `engine_inference.go:43`, `summarizer.go:57`, `engine_execution.go:33`, `engine_phases.go:146`, `gatekeeper.go:177,283`, `agent.go:242`, `engine.go:308` | ✅ Safe — `SafePublish` deterministic check unchanged |
| **Tests ×40** | All test files | `bridge_test.go`, `event_queue_test.go`, etc. | ✅ Safe — co-delivered with PR #230 |
| **Integration ×2** | Both HandleEvent calls | `spinner_integration_test.go:227,256` | ✅ Safe — contexts alive at call time |
| **Out-of-tree** | cmd/, pkg/ | (none) | ✅ Safe — zero callers |
| **TurnsLogger** | Separate interface | `session_manager.go:271` | ✅ Safe — different contract, no enqueue |

### Acceptance Criteria Checklist

- [x] Task 1: Subscriber-side analysis → `docs/audits/issue-232-task1.md`
- [x] Task 2: Producer-side analysis → `docs/audits/issue-232-task2.md`
- [x] Task 3: Test surface analysis → `docs/audits/issue-232-task3.md`
- [x] Task 4: Integration test surface → `docs/audits/issue-232-task4.md`
- [x] Task 5: Final cross-cutting check → this file
- [x] All 14 call sites classified: 1 ⚠️, 13 ✅
- [x] ADR update prepared → `docs/audits/issue-232-audit-summary.md`
- [x] Test suite verified: `go test -race -count=10 ./internal/agent/session/...` → PASS
- [x] Test suite verified: `go test -race -count=10 ./tests/integration/agent/session/...` → PASS
- [x] Task 1 diff ready to apply (option b: downgrade `context.Canceled` to Debug)

### Resolution Comment (Draft)

---

**Audit complete. All 5 tasks finished.**

### Outcome

The `enqueueNonCritical` contract change (PR #230) is **safe to accept** with one targeted fix.

**Call site classification:**
- ✅ **13 safe** — 8 publishers, 40 tests, 2 integration calls, 2 out-of-tree checks, 1 TurnsLogger
- ⚠️ **1 behavioral drift** — subscriber lambda at `session_manager.go:295`

### Fix

The subscriber lambda at `session_manager.go:295` now receives `context.Canceled` during shutdown where it previously got `nil` (via random `select` luck). The fix downgrades this expected shutdown signal from `Warn` to `Debug`:

```diff
--- a/internal/agent/session/session_manager.go
+++ b/internal/agent/session/session_manager.go
@@ -294,7 +294,11 @@ func (o *sessionManager) setupUIRendering(...)
 	chatAgent.Subscribe(func(ctx context.Context, e events.Event) {
 		if err := bridge.HandleEvent(ctx, e); err != nil {
-			logger.Warn("Failed to handle bridge event", "error", err, "event", fmt.Sprintf("%T", e))
+			if errors.Is(err, context.Canceled) {
+				logger.Debug("Bridge event skipped: context cancelled", "event", fmt.Sprintf("%T", e))
+			} else {
+				logger.Warn("Failed to handle bridge event", "error", err, "event", fmt.Sprintf("%T", e))
+			}
 		}
 	})
```

### Verification

```
go test -race -count=10 ./internal/agent/session/...     → PASS (3 packages, ~10s)
go test -race -count=10 ./tests/integration/agent/session/... → PASS (13.8s)
```

### ADR

Issue #231 can move from `Proposed` → `Accepted`. The consolidated audit summary is at `docs/audits/issue-232-audit-summary.md`.

### Follow-up

One gap identified: no integration test covers the full shutdown cascade (orchestrator → EventBus subscriber → bridge → spinner). Existing unit tests cover this path. A follow-up integration test would improve coverage but is not a blocker.

---

