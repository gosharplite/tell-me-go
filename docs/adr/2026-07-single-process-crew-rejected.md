# ADR-045: Single-Process Crew — Rejected

- **Status:** Rejected
- **Date:** 2026-07
- **Author:** Architect (tell-me-go)

## Context

The Architect proposed formalizing the project's multi-agent collaboration into a single-process `Crew` abstraction with in-process `HandoffBus`, shared `CrewState`, and typed inter-agent messages — replacing the current temp-file-based message passing between separate `tell-me-go` processes.

At the time of the proposal, the project had:

- **~59K LOC** across 72 packages
- **98.7% test coverage**
- **Clean architecture** with zero circular dependencies or layer violations
- **Two production SOPs** orchestrating multi-agent workflows:
  - `docs/sop/lifecycle/issue_to_pr_orchestration.md` — 3 agents (Orchestrator, Architect, Coder), 4 phases
  - `docs/sop/lifecycle/pr_review.md` — 4 agents (Orchestrator, Architect-A, Coder-A, Architect-B) across two LLM providers (deepseek-pro + vertex-pro), 5 phases
- **Tekton pipelines** executing these SOPs in CI/CD

The proposal was evaluated against the following question:

> Is the temp-file-based message passing between separate `tell-me-go` processes worth formalizing into a single-process `Crew`?

## Decision

**Do not pursue a single-process `Crew`.** The current architecture — SOP `.md` files as the protocol, independent `tell-me-go` processes as agents, Tekton as the orchestrator, and temp files as transport — is the correct design for the project's constraints. The 60K LOC result at high quality is validation of the approach.

## Analysis

### 1. What the Current Architecture Provides (for free)

| Property | Mechanism | Value |
|----------|-----------|-------|
| **Process isolation** | Each agent is an independent OS process | Architect crash cannot corrupt Coder's session. Failure domains are naturally separated. |
| **Cross-provider support** | Each process has its own `--config` with a single `SELECTED_PROVIDER` | `pr_review.md` runs Architect-A on deepseek-pro and Architect-B on vertex-pro simultaneously with zero code changes. A single-process Crew would require multi-provider wiring in one binary — a significant refactor of the `Chatter` / `LLMGateway` binding. |
| **Tekton-native execution** | Each `tell-me-go` invocation is a pipeline step | Tekton handles retries, timeouts, logging, and artifact passing. Temp files are Tekton workspace volumes. Collapsing into one long-running process would lose step-level observability and retry granularity. |
| **Explicit session model** | `--new` vs persistent sessions are CLI flags | No in-process state machine needed. The Orchestrator SOP explicitly controls when sessions reset. |
| **Independent debugging** | `tell-me-go -t -c ${ROLE_CONFIG}` on any role | Operators can inspect any agent's transcript independently. A single-process Crew would require a unified trace with filtering — more infrastructure for no net gain. |
| **Proven at scale** | The system built itself | 60K LOC, 98.7% coverage, 44 ADRs, zero circular dependencies. The architecture works. |

### 2. What a Single-Process Crew Would Require

| Required Change | Complexity |
|-----------------|------------|
| **Multi-provider `Chatter`** | The current `Chatter` binds one `LLMGateway` per instance (`ports.ChatterComposer.GetGateway()` returns a single gateway). A Crew holding multiple agents would need to hold multiple gateways simultaneously. This is a fundamental assumption in `internal/infrastructure/di/container.go` and `internal/infrastructure/factory/chatter.go`. |
| **In-process session lifecycle** | Replace `--new` (fresh OS process + config load + session init) with an in-process state machine that resets Coder sessions while keeping the Crew alive. The current session model is coupled to process lifetime. |
| **Cross-provider handoff** | The `HandoffBus` would need to route messages between agents using different providers — meaning different `LLMGateway` instances, different `TokenCounter` instances, different pricing models. This is already solved by separate processes each with their own config. |
| **Shared state persistence** | A `CrewState` with `TaskBoard`, `ArtifactStore`, and shared path authorization would need SQLite-backed persistence (extending `sqlite_store.go` patterns) and concurrency control across agents. |
| **ADR overhead** | Multi-provider session management, in-process handoff protocol, Crew state persistence, and Tekton pipeline restructuring would each require an ADR. The current SOP `.md` files already serve as the protocol documentation — they are effectively "executable ADRs." |

### 3. The Cost/Benefit Equation

| | Current (temp-file) | Proposed (single-process Crew) |
|---|---|---|
| **Implementation effort** | Already built and proven | ~4-6 weeks of refactoring across `di`, `factory`, `agent`, and new `crew` package |
| **Risk to existing functionality** | None | High — touches core `Chatter` lifecycle and provider binding |
| **Tekton compatibility** | Native | Requires pipeline redesign |
| **Cross-provider reviews** | Trivial (separate configs) | Requires architectural change to `LLMGateway` binding |
| **Debugging ergonomics** | Independent per-agent traces | Requires unified tracing infrastructure |
| **Perceived benefit** | — | Faster handoffs (microseconds vs process spawn), shared state |

The primary benefit — faster handoffs — is not a bottleneck in Tekton pipelines where step startup time is amortized over the entire turn execution (LLM inference dominates, not process spawn). Shared state is achievable incrementally without collapsing processes (see Consequences below).

### 4. Precedent in Architecture Decision Records

The project has a pattern of rejecting proposals when the benefit does not justify the complexity:

- **ADR-039 (WarmImplementations)**: Rejected because "no consumer has demonstrated measurable UX impact" from the 5-second TTL sawtooth, and "background goroutine lifecycle management adds complexity disproportionate to the benefit."
- **ADR-021 (MockEstimator collapse)**: Rejected because collapsing would "force MockTokenCounter consumers to depend on a wider interface surface."

This proposal follows the same pattern: the temp-file approach is not broken, and the complexity of the alternative outweighs the benefit.

## Consequences

### Positive

- **No unnecessary refactoring.** The core `Chatter` / `Engine` / `Dispatcher` architecture remains stable.
- **Cross-provider workflows remain simple.** Separate `--config` per agent is a well-understood pattern.
- **Tekton pipelines are unchanged.** No pipeline redesign needed.
- **Documented rejection.** Future contributors can reference this ADR rather than re-proposing the same idea.

### Negative

- **Handoff latency:** Temp-file + process spawn adds ~200-500ms per handoff. Not material in Tekton pipelines where LLM inference dominates, but would matter in interactive use. Mitigation: not a current use case.
- **No shared CrewState:** Agents cannot share task lists, authorized paths, or artifacts without the Orchestrator explicitly forwarding them. Partially mitigated by the Orchestrator's session memory, which accumulates this state naturally during SOP execution.

### Recommended Incremental Improvements (Not Blocked by This Rejection)

1. **Handoff validation**: A lightweight tool or script that validates `TASK:`/`REVISION:`/`DONE.` prefix formatting before the Orchestrator forwards to the target agent. Reduces silent failures from malformed model output.
2. **Shared state file**: A JSON or SQLite artifact that persists between Tekton steps, holding the task board, authorized paths, and analysis summaries. The Orchestrator reads/writes it; sub-agents don't need to know it exists. This achieves most of the `CrewState` value without process collapse.
3. **Token monitoring automation**: The Orchestrator SOP already specifies manual `-t | tail -5` checks. A helper that auto-checks after N cycles and warns the Orchestrator would reduce cognitive load without changing the architecture.

## References

- [ADR-039: Lazy Implementation Index](../adr/2026-05-lazy-implementation-index.md) — "Rejected: WarmImplementations(ctx) Opt-In" section
- [ADR-021: Test Doubles via `*test` Sub-Packages](../adr/2026-04-test-doubles-in-pkgtest-subpackages.md) — MockEstimator collapse rejection
- [Issue-to-PR Orchestration SOP](../sop/lifecycle/issue_to_pr_orchestration.md)
- [PR Review SOP](../sop/lifecycle/pr_review.md)
- [Chatting with AI Protocol](../../docs/steps/chatting-with-ai.md)
