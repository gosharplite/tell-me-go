# ADR-076: Asynchronous Webhook Callback Worker Pattern (--callback)

**Status:** Accepted
**Date:** 2026-09
**Related:** [Issue #1483](https://github.com/gosharplite/tell-me-go/issues/1483), [Issue #1479](https://github.com/gosharplite/tell-me-go/issues/1479) (superseded), [ADR-075](2026-09-scalability-boundary-conditions.md), [ADR-074](2026-09-process-runner-injection.md), [ADR-064](2026-08-ports-shared-kernel-registry-gate.md), [ADR-056](2026-08-contract-home-and-transitive-closure-gate.md)

## Context

In production workflow automation (e.g. n8n orchestrating cluster operations via SSH/HTTP, Airflow, CI/CD runners, and webhook dispatchers), agentic multi-turn reasoning and tool executions routinely take 30–180+ seconds. Holding a synchronous CLI execution channel locks orchestrator worker threads and triggers 504 Gateway Timeouts across ingress load balancers.

Decoupling intake from execution and delivery enables an **Asynchronous Webhook Callback Worker Pattern**:
1. An orchestrator invokes `tell-me-go` with a prompt and a webhook callback URL (`--callback`).
2. `tell-me-go` performs fast pre-flight validation, acquires a concurrency lock on the mode, captures the prompt, outputs an immediate acknowledgment line (`ACK <session_id>\n`), closes standard output to signal EOF to the caller pipe, and releases the caller.
3. `tell-me-go` executes inference in the foreground while streaming live telemetry to `stderr`.
4. Upon reaching any terminal state (completion, error, cancellation, panic), `tell-me-go` dispatches a standardized JSON payload to the `--callback` URL and exits.

### Historical Context and Ratified Amendments (Superseding #1479)

The initial proposal in Issue #1479 was pressure-tested during an adversarial grill round (Architect vs Griller, 7 questions, code-verified evidence), producing **7 binding amendments** ratified on 2026-09-05 and formalized in Issue #1483:

| # | #1479 Proposal | Ratified Amendment (Verified Evidence) |
|---|---|---|
| **A1** | Notify hook attached at `ProcessMessage` turn completion | **CLI-layer terminal wrapper** in `executeChat` covers **all** post-ACK returns (including early returns in `BuildSessionDependencies` which bypass `finalizeSessionState`). `Notify` runs on a fresh `context.Background()` with 15s timeout. Post-ACK `defer recover()` delivers an error payload then **re-panics** (preserving exit code 2). Pre-flight failure = documented no-ACK contract. SIGKILL/OOM is explicitly out-of-envelope. |
| **A2** | Isolation as documentation only ("one MODE per slot") | **Code-enforced fail-fast lock**: pre-ACK, `.mode.lock` file in the mode directory via platform-split file locks (`flock` on POSIX / `LockFileEx` on Windows). Contended -> stderr reason + exit 1, **no ACK**. Parallel slots recipe: `TELL_ME_MODE=worker-{{ $execution.id }}` for N isolated mode directories. |
| **A3** | Re-homing response streams to stderr | **Writer topology verified**: tool traces, turn status, metrics, token counts, and spinners are **already stderr-bound** (`internal/ui/renderer*.go`); the response body is the sole stdout-bound stream. Consequence: early stdout detach costs only the response body (which is delivered in the webhook payload and persisted in `history.jsonl` / `turns.log`). |
| **A4** | Implicit flag blacklist | **Whitelist guard** via `cmd.Flags().VisitAll` — every `Changed()` flag must be in `{"config", "new", "callback", "callback-id", "callback-header"}`; anything else -> pre-flight rejection (exit 1, no ACK). Placed immediately after config load, strictly before all early-return branches. `BYPASS_CONFIRMATION: true` is config-only (no CLI flag). |
| **A5** | Port in `internal/domain/ports/callback.go`, ADR-064 family 9, notifier injected via `ports.ChatServiceConfig` | **Dead on arrival** per ADR-064 (folds, never mints) and ADR-075. Port `CallbackNotifier` + `CallbackPayload` placed in **`internal/domain/callback`**; mode lock port in **`internal/domain/persistence`**; both injected via `cli.AppDependencies`. `ChatService` remains completely **callback-agnostic**. Redirect writer constructed in `buildApp` living in `internal/pkg/redirectwriter`. |
| **A6** | Correlation ID resolution without validation | **Wire-protocol string validation — validated, never sanitized**: `--callback-id` regex **`^[A-Za-z0-9._:-]{1,128}$`**. Absent flag -> generated `session-<16 hex>` via `internal/pkg/idgen`; present-but-empty -> rejected. |
| **A7** | Error-path response payload implied `""` by example only | **Invariant: `response` is non-empty if and only if `status == "success"`**. On error, `response` is strictly `""` and `GetLastModelTurn` is **never queried** (preventing stale responses from previous turns under atomic append from leaking). |

## Decision

We adopt the 10 binding decisions (D1–D10) governing the callback worker execution model:

### D1: Early Stdout Close via `redirectwriter.Writer`
The stdout OS file descriptor must actually close so piped callers observe `EOF` immediately. `internal/pkg/redirectwriter` provides an atomic-target `io.Writer` wrapping the underlying stdout with `Detach()`. In `executeCallbackWorkflow`, writing `ACK <session_id>\n` is followed immediately by `Detach()`, which flushes the writer, closes the base file descriptor, and atomically swaps the active target to `io.Discard`. Non-callback chat execution never calls `Detach()`, preserving standard terminal behavior.

### D2: `BYPASS_CONFIRMATION: true` Config Guard
Callback worker mode requires non-interactive execution. `cfg.BypassConfirmation` must be `true` in the configuration file. If `false` or unconfigured, `tell-me-go` fails fast before ACK (stderr reason, exit code 1, no ACK). Prompt-freedom post-ACK is guaranteed by the A4 whitelist guard, preventing interactive prompts from blocking background execution.

### D3: Cross-Platform Fail-Fast Mode Lock
To prevent concurrent invocations from corrupting session files (`history.jsonl`, `turns.log`, `tellmego.db`), callback mode enforces exclusive file locking on `<mode_dir>/.mode.lock`. Implemented via `internal/infrastructure/persistence/lock_posix.go` (`flock(LOCK_EX|LOCK_NB)`) and `lock_windows.go` (`LockFileEx`), contention fails fast (stderr reason, exit code 1, no ACK). The lock is held throughout process execution and released upon termination. Parallel orchestrator slots are achieved without config proliferation via `TELL_ME_MODE=worker-{{ $execution.id }}`.

### D4: Early ACK and Response Body Redirection
The stdout discriminator contract guarantees that the first and only line on stdout is `ACK <session_id>\n` followed by EOF. Piped callers parse this line as acceptance. Because stdout is detached to `io.Discard`, the streaming model response is absorbed, while all diagnostic telemetry remains visible on `stderr`.

### D5: CLI-Layer Terminal Wrapper
The terminal wrapper in `internal/cli/chat_command.go` wraps inference execution (`c.ChatService.ProcessMessage`) and captures all post-ACK terminal states:
- **Success**: Model completes successfully.
- **Inference Error**: Model context overflow, provider rate-limit, or runtime error.
- **Context Cancellation**: SIGTERM or external process interruption.
- **Panic**: Recovered via `defer recover()`, formatting an error payload, firing the webhook notification, and then re-panicking with the original value.

### D6: Single POST Attempt with 15s Hard Timeout
Webhook delivery is dispatched as a single HTTP POST with a 15-second hard timeout (`context.WithTimeout(context.Background(), 15*time.Second)`), operating on an independent background context so canceled session contexts do not abort notification. Webhook delivery failure (network timeout or non-2xx status code) writes an error to `stderr` and exits with code 1.

### D7: Wire-Protocol String Validation
All caller-supplied strings binding to the wire protocol are strictly validated before execution:
- **Callback URL**: Validated via `url.Parse` to enforce scheme `http` or `https` and non-empty host (SSRF guard).
- **Callback Headers**: Format `Name: Value`. `Name` must be a valid RFC 7230 token (`^[!#$%&'*+\-.^_` + "`" + `|~a-zA-Z0-9]+$`). `Value` rejects `\r` and `\n` (CRLF injection prevention).
- **Telemetry Masking**: Sensitive header values matching `(?i)(auth|token|key|secret|cookie|pass|cred)` are masked as `***` (or `Bearer ***`) in all stderr telemetry.

### D8: Closed Payload Contract and Status-Aware Assembly
The webhook payload schema is closed:
```json
{
  "session_id": "string",
  "status": "success | error",
  "response": "string",
  "error": "string | null"
}
```
- When `status == "success"`: `error` is `null`, and `response` contains the concatenated non-thought text parts (`!p.IsThought && p.Text != ""`) extracted from `hManager.GetLastModelTurn(ctx)`.
- When `status == "error"`: `response` is strictly `""` (never querying `GetLastModelTurn`), and `error` contains the aggregated error message string.

### D9: Stderr Telemetry Preserved
Diagnostic telemetry on `stderr` (tool calls, spinner progress, token metrics, turn status) remains intact and untouched. Callers monitoring `stderr` continue to receive real-time execution logs.

### D10: Correlation ID Resolution and Validation
If `--callback-id` is provided, it must match regex `^[A-Za-z0-9._:-]{1,128}$`; present-but-empty values are rejected. If omitted, `tell-me-go` generates a session ID using `internal/pkg/idgen.Generate()`, maintaining the standard `session-<16 hex>` format.

---

## Orchestrator Contract & Exit Code Matrix

**Stdout Discriminator:**
| Observation | Meaning |
|---|---|
| `ACK <id>\n` then EOF | Request accepted. Wait for webhook delivery. |
| Anything else (empty, usage error, stderr output) | Pre-flight rejection. Webhook will not fire; fail workflow node immediately. |

> **Note on root flags**: Cobra intercepts root-level flags (`--version`, `--help`) before command `RunE` executes. An invocation like `tell-me-go --version --callback <url> <prompt>` outputs version text and exits 0 without emitting `ACK`. This conforms to the stdout discriminator contract: the orchestrator observes version or usage text rather than `ACK <session_id>\n`, correctly treating it as a non-started invocation.

**Exit Codes:**
| Scenario | Webhook Delivery | Process Exit Code |
|---|---|---|
| Pre-flight validation failure (D2, whitelist, URL, header, ID, lock, prompt) | None | 1 |
| Post-ACK inference success + 2xx delivery | `status: "success"` | 0 |
| Post-ACK inference failure + 2xx delivery | `status: "error"` | 0 |
| Context canceled (SIGTERM) + 2xx delivery | `status: "error"` | 0 |
| Webhook delivery failure (non-2xx or unreachable) | Attempted, failed | 1 |
| Post-ACK panic | `status: "error"` delivered first | 2 (re-panic) |
| SIGKILL / OOM | None | Out of envelope (orchestrator wait timeout) |

---

## Consequences

### Positive
- **Workflow Decoupling**: Orchestrators (n8n, Airflow) receive an immediate early-ACK and release resources while `tell-me-go` executes in background.
- **Global Stdout Passthrough**: In `cmd/tell-me-go/main.go` (`buildApp`), `redirectwriter.Writer` wraps `stdout` globally across all modes as an atomic passthrough. In standard interactive and streaming executions, it acts as a zero-overhead passthrough; in callback mode, it enables early `Detach()` to close the underlying OS file descriptor while cleanly absorbing trailing writes via `io.Discard`.
- **Clean Architecture Adherence**: `ChatService` and `domain/ports` remain completely callback-agnostic. Ports live in `internal/domain/callback` and `internal/domain/persistence`.
- **Strict Concurrency Safety**: Exclusive mode lock prevents concurrent process corruption.
- **Fail-Fast Observability**: Pre-flight validation rejects invalid inputs before execution begins, guaranteeing that an ACKed execution will attempt delivery.
- **Forensic Integrity**: Full conversation history and turn diagnostics remain persisted in `history.jsonl` and `turns.log`.

### Negative & Neutral
- Webhook delivery is single-attempt with 15s timeout; retries are delegated to orchestrator design.
- The `--callback-id` charset is closed to `^[A-Za-z0-9._:-]{1,128}$`; widening requires evidence-backed ADR amendment.
- Transitive import whitelist updated for `cmd/tell-me-go`, `internal/cli`, and `internal/cli/clitest` to allow the `callback` domain family.

## References

- [Issue #1483](https://github.com/gosharplite/tell-me-go/issues/1483) — Asynchronous Webhook Callback Worker specification.
- [Issue #1479](https://github.com/gosharplite/tell-me-go/issues/1479) — Original proposal (superseded).
- [Grill-Round Transcript](https://gist.github.com/gosharplite/3d7b2b00d2804c0146b1241863dc8eac) — Verification and amendment ledger.
- [ADR-075](2026-09-scalability-boundary-conditions.md) — Boundary conditions and registry headroom.
- [ADR-074](2026-09-process-runner-injection.md) — Process runner injection.
- [ADR-064](2026-08-ports-shared-kernel-registry-gate.md) — Ports shared-kernel registry gate.
- [ADR-056](2026-08-contract-home-and-transitive-closure-gate.md) — Transitive closure gate.
