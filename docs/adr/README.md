# Architectural Decision Records (ADRs)

This directory contains Architectural Decision Records (ADRs) documenting significant architectural decisions in the `tell-me-go` project. ADRs follow the format specified in [ADR Standards](../sop/standards/adr_standards.md).

## ADR Index

| ADR # | Title | Date | Status | File |
|-------|-------|------|--------|------|
| **ADR-001** | Hybrid LLM Infrastructure Strategy | 2026-01 | Accepted | [2026-01-multi-llm-provider-strategy.md](2026-01-multi-llm-provider-strategy.md) |
| **ADR-002** | Standardize Tool Execution Concurrency, Timeouts, and Context Propagation | 2026-01 | Accepted | [2026-01-tool-execution-concurrency-and-timeouts.md](2026-01-tool-execution-concurrency-and-timeouts.md) |
| **ADR-003** | Domain Decomposition of Chatter Orchestrator | 2026-01 | Accepted | [2026-01-domain-decomposition-strategy.md](2026-01-domain-decomposition-strategy.md) |
| **ADR-004** | Elimination of ChatterParams God Object | 2026-01 | Accepted | [2026-01-chatterparams-elimination.md](2026-01-chatterparams-elimination.md) |
| **ADR-005** | Skill Injection Architecture | 2026-01 | Accepted | [2026-01-skill-injection-architecture.md](2026-01-skill-injection-architecture.md) |
| **ADR-006** | History Log Compaction and Bounded Contexts | 2026-01 | Implemented | [2026-01-history-log-compaction.md](2026-01-history-log-compaction.md) |
| **ADR-007** | Extract Agent Configuration via Functional Options | 2026-02 | Accepted | [2026-02-agent-options-extraction.md](2026-02-agent-options-extraction.md) |
| **ADR-008** | Bubble Tea Interactive History Browser | 2026-02 | Implemented | [2026-02-bubble-tea-history-browser.md](2026-02-bubble-tea-history-browser.md) |
| **ADR-009** | TUI Interactive Prompt Mode with Auto-completion | 2026-02 | Implemented | [2026-02-tui-prompt-mode.md](2026-02-tui-prompt-mode.md) |
| **ADR-010** | Accept Dual-Write for Telemetry and UI Events | 2026-04 | Accepted | [2026-04-accept-dual-write-for-telemetry-events.md](2026-04-accept-dual-write-for-telemetry-events.md) |
| **ADR-011** | Refactor UI Bridge to Actor Model for Thread-Safe Asynchronous Rendering | 2026-04 | Accepted | [2026-04-uibridge-actor-model.md](2026-04-uibridge-actor-model.md) |
| **ADR-012** | Dynamic Tool Discovery via Capability Toolkits | 2026-04 | Accepted | [2026-04-dynamic-tool-discovery.md](2026-04-dynamic-tool-discovery.md) |
| **ADR-013** | Asynchronous Event-Driven Orchestration | 2026-04 | Accepted | [2026-04-asynchronous-event-driven-orchestration.md](2026-04-asynchronous-event-driven-orchestration.md) |
| **ADR-014** | Event-Driven Orchestration and Circuit Breaker Pipeline | 2026-04 | Accepted | [2026-04-event-driven-orchestration.md](2026-04-event-driven-orchestration.md) |
| **ADR-015** | Cross‑Platform Timing Guarantees and HTTP‑Streaming Duration Measurement | 2026-04 | Accepted | [2026-04-cross-platform-timing-guarantees.md](2026-04-cross-platform-timing-guarantees.md) |
| **ADR-016** | Migrate Persistent State to Pure Go SQLite | 2026-02 | Implemented | [2026-02-pure-go-sqlite-migration.md](2026-02-pure-go-sqlite-migration.md) |
| **ADR-017** | Black-Box Integration Test Tree at `tests/` | 2026-04 | Accepted | [2026-04-blackbox-integration-test-tree.md](2026-04-blackbox-integration-test-tree.md) |
| **ADR-018** | Migrate Orchestrator to Event-Driven Actor Model | 2026-04 | Accepted | [2026-04-event-driven-actor-model.md](2026-04-event-driven-actor-model.md) |
| **ADR-019** | JSONL History Persistence | 2026-04 | Accepted | [2026-04-jsonl-history-persistence.md](2026-04-jsonl-history-persistence.md) |
| **ADR-020** | Windows Compatibility Strategy | 2026-04 | Accepted | [2026-04-windows-compatibility-strategy.md](2026-04-windows-compatibility-strategy.md) |
| **ADR-021** | Test Doubles Live in `*test` Sub-Packages, Not a Centralized `testutil` | 2026-04 | Accepted | [2026-04-test-doubles-in-pkgtest-subpackages.md](2026-04-test-doubles-in-pkgtest-subpackages.md) |
| **ADR-022** | Tool-Result Error Convention | 2026-04 | Accepted | [2026-04-tool-result-error-convention.md](2026-04-tool-result-error-convention.md) |
| **ADR-023** | Reasoning-Token Accounting for OpenAI-Compatible Providers | 2026-04 | Accepted | [2026-04-reasoning-token-accounting.md](2026-04-reasoning-token-accounting.md) |
| **ADR-024** | OpenAI Chat-vs-Responses Budget-Field Divergence | 2026-04 | Accepted | [2026-04-openai-budget-field-divergence.md](2026-04-openai-budget-field-divergence.md) |
| **ADR-025** | Decompose UIBridge God Object into Composable Sub-Components | 2026-04 | Accepted | [2026-04-uibridge-decomposition.md](2026-04-uibridge-decomposition.md) |
| **ADR-026** | Decompose `agent/session` Package via `context/` Sub-Package Extraction | 2026-04 | Accepted | [2026-04-session-context-subpackage-extraction.md](2026-04-session-context-subpackage-extraction.md) |
| **ADR-027** | Continue Sub-Package Extraction — Integrations (ado/atlassian) and Session (ui/) | 2026-05 | Accepted | [2026-05-integrations-and-session-ui-subpackage-extraction.md](2026-05-integrations-and-session-ui-subpackage-extraction.md) |
| **ADR-028** | Delegate Registration Logic to Integration Sub-Packages | 2026-05 | Accepted | [2026-05-subpackage-registration-pattern.md](2026-05-subpackage-registration-pattern.md) |
| **ADR-029** | Fallible `Reconfigure` Delegate Chain in Agent Hot-Reload | 2026-05 | Accepted | [2026-05-fallible-reconfigure-delegate-chain.md](2026-05-fallible-reconfigure-delegate-chain.md) |
| **ADR-030** | Release Branch Synchronization Policy | 2026-05 | Accepted | [2026-05-release-branch-synchronization-policy.md](2026-05-release-branch-synchronization-policy.md) |
| **ADR-031** | Caller Cancellation Priority in Dual-Context Enqueue | 2026-05 | Accepted | [2026-05-caller-cancellation-priority.md](2026-05-caller-cancellation-priority.md) |
| **ADR-032** | Test-Hook Policy for Unexported Concrete Types | 2026-05 | Accepted | [2026-05-test-hook-policy.md](2026-05-test-hook-policy.md) |
| **ADR-033** | Narrow the `auth.Authenticator` Interface (Remove `getToken`) | 2026-05 | Accepted | [2026-05-narrow-authenticator-interface.md](2026-05-narrow-authenticator-interface.md) |
| **ADR-034** | Cosmetic Lint Fixes Must Be Submitted as Standalone PRs | 2026-05 | Accepted | [2026-05-cosmetic-lint-fix-pr-separation.md](2026-05-cosmetic-lint-fix-pr-separation.md) |
| **ADR-035** | Per-Request Goroutine Model for Capturer Reads | 2026-05 | Accepted | [2026-05-capturer-per-request-goroutine.md](2026-05-capturer-per-request-goroutine.md) |
| **ADR-036** | Test Determinism Standards (No-Sleep, Race-Safe Mocks, Deterministic Time) | 2026-05 | Accepted | [2026-05-test-determinism-standards.md](2026-05-test-determinism-standards.md) |
| **ADR-037** | Test-Only Access via `agentinternal` Bridge & `*ForInternalUse` Branding | 2026-04 | Accepted | [2026-04-test-only-access-via-agentinternal-bridge.md](2026-04-test-only-access-via-agentinternal-bridge.md) |
| **ADR-038** | astCache Path Resolution via Injected baseDir | 2026-05 | Accepted | [2026-05-astcache-path-resolution.md](2026-05-astcache-path-resolution.md) |
| **ADR-039** | Lazy Implementation Index — eager→lazy contract change for `computeImplementations` | 2026-05 | Accepted | [2026-05-lazy-implementation-index.md](2026-05-lazy-implementation-index.md) |
| **ADR-040** | Complete Session Subpackage Extraction (config_watcher, skill_injector) | 2026-05 | Accepted | [2026-05-session-extraction-completion.md](2026-05-session-extraction-completion.md) |
| **ADR-041** | DI Composition Root — Sub-Provider Decomposition | 2026-05 | Accepted | [2026-05-di-composition-root-decomposition.md](2026-05-di-composition-root-decomposition.md) |
| **ADR-042** | Pipeline Presentation Extraction via PipelineFormatter Interface | 2026-05 | Accepted | [2026-05-ado-pipeline-formatter.md](2026-05-ado-pipeline-formatter.md) |

## How to Create a New ADR

1. Review the [ADR Standards](../sop/standards/adr_standards.md)
2. Create a new file with naming convention: `YYYY-MM-short-descriptive-title.md`
3. Follow the required ADR format (Status, Context, Decision, Consequences)
4. Update this README.md file with the new ADR entry
5. Update relevant project documentation (README.md, ROADMAP.md) with references

## Purpose of ADRs

ADRs capture important architectural decisions made along the project's evolution. They help:

- **Preserve Context**: Document why a particular approach was chosen over alternatives
- **Avoid Regressions**: Prevent re-discussion of settled architectural questions  
- **Onboard New Contributors**: Provide historical context for design choices
- **Maintain Consistency**: Ensure new decisions align with established architecture

## Related Documentation

- [ADR Standards](../sop/standards/adr_standards.md) - Process for creating and maintaining ADRs
- [README.md](../../README.md) - Main project documentation with ADR references
- [ROADMAP.md](../../ROADMAP.md) - Project roadmap with ADR implementation status
