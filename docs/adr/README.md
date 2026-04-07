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
