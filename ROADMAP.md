# tell-me-go Project Roadmap

This document outlines the strategic evolution of `tell-me-go`. Our primary goal is to provide a unified, provider-agnostic interface for high-performance reasoning models.

- **Strategic Strategy:** [ADR-001: Hybrid LLM Infrastructure Strategy](./docs/adr/2024-05-multi-llm-provider-strategy.md)
- **Technical Specification:** [Multi-Provider Implementation Plan](./docs/sop/technical/multi_provider_implementation.md)
- **Design Standards:** [ADR Management SOP](./docs/sop/standards/adr_standards.md)

## Phase 1: Foundation & Governance (Completed)
- [x] Documentation & ADR Initialization
- [x] ADR-001: Hybrid Infrastructure Strategy
- [x] Refactor Domain `llm.Part`: Standardize `Thought` architecture as a unified boolean-based thinking model.
- [x] Implement **Registry-based Configuration**:
    - [x] Define `LLMProvider` struct in domain.
    - [x] Update `Config` to support `Providers` map and `SelectedProvider` key.
- [x] Implement **Recursive Environment Expansion**:
    - [x] Support `${VAR}` syntax in YAML for secret injection (API Keys).
- [x] Implement **Dynamic Provider Factory**:
    - [x] Decouple client creation from `chat_command.go`.

## Phase 2: OpenAI-Compatible Infrastructure (Completed)
- [x] Implement internal/infrastructure/llm/openai:
    - [x] Manual HTTP transport for OpenAI v1 Chat Completion.
    - [x] Specific mapping for gpt-5.2 (reasoning_tokens) and deepseek-reasoner (reasoning_content).
- [x] Standardize ResilientClient to support custom HTTP headers, bearer tokens, and standardized error classification via `llmerr.APIError`.
- [x] Implement SSE Streaming Support for OpenAI-compatible providers.

## Phase 2.5: Anthropic (Claude) Infrastructure (Completed)
- [x] Implement internal/infrastructure/llm/anthropic:
    - [x] Manual HTTP transport for Anthropic Messages API (/v1/messages).
    - [x] Specific mapping for 'type: thinking' content blocks to domain 'Thought' string.
    - [x] Support for 'x-api-key' and 'anthropic-version' headers.
- [x] Implement SSE Streaming Support for Anthropic-specific event protocol.

## Phase 3: Orchestration & Telemetry (Completed)
- [x] Dynamic Provider Registry (Factory Pattern implementation in `internal/infrastructure/llm/factory.go`)
- [x] Multi-provider Token & Cost tracking (Supporting Thinking/Reasoning rates for 2026 models)
- [x] Config-driven provider switching (Support for `SELECTED_PROVIDER` and `PROVIDERS` map)

## Phase 4: Optimization & Scalability (Completed)
- [x] Gemini Integration: Robust native implementation (`internal/infrastructure/llm/gemini`).
- [x] History Log Compaction (ADR-006): Implemented `jsonlStore` patches and persistent archiving to solve memory/OOM bloat on long-running sessions.

## Phase 5: Advanced Capability (Completed)
- [x] Silent Observability: Implement conditional OpenTelemetry exporter integration in `main.go` using `TELL_ME_TRACE_ENDPOINT`.

## Phase 6: Domain Decomposition & Scalability (Completed)
- **Goal:** Address Single Responsibility Principle (SRP) violations, decompose God Objects, and standardize tool execution.
- **Task:** Implement **High-Performance Tool Execution Engine** with concurrency and standardized timeouts. (COMPLETED - See [ADR-002](./docs/adr/2024-06-tool-execution-concurrency-and-timeouts.md))
- **Task:** Implement **Domain Decomposition Strategy** to separate core logic from infrastructure and UI. (COMPLETED - See [ADR-003](./docs/adr/2024-07-domain-decomposition-strategy.md))
- **Task:** Investigate the `Chatter` domain service. It currently accepts a massive `ChatterParams` struct with 13 dependencies. (COMPLETED - See [ADR-004](./docs/adr/2024-08-chatterparams-elimination.md))
- **Task:** Evaluate splitting `Chatter` into distinct pipeline stages (e.g., `ContextBuilder`, `ExecutionEngine`, `ResponseProcessor`) or applying the Facade Pattern. (COMPLETED)
- **Task:** Continuous dependency injection audit to ensure the composition root (`container.go`) remains clean. (COMPLETED - Extracted to `internal/infrastructure/factory/chatter.go`)

## Phase 7: Dynamic Knowledge & Skills (Completed)
- **Goal:** Optimize context usage by dynamically injecting Go-specific development patterns.
- **Task:** Implement **Skill Injection Architecture** (ADR-005):
    - [x] Create in-memory `SkillRepository` with file-system caching.
    - [x] Implement token-aware `SkillSelector` for dynamic relevance-based injection.
    - [x] Integrate `SkillSelector` into the `ContextPipeline` to provide contextual Go patterns without context bloat.

---
*Note: This roadmap is subject to change based on the evolution of LLM APIs and project requirements.*


## Phase 8: Interactive History Exploration (Proposed)
- **Goal:** Provide professional-grade, unbounded history exploration with search, navigation, and thought visibility controls, without compromising memory safety or LLM context limits.
- **Task:** Implement **Unified History Data Layer (Strict Prerequisite - STOP & VERIFY before TUI)**:
    - [x] **1. Domain Layer (Read Model)**: Define `HistoryViewDTO`, `ArchiveReader`, and `UnifiedHistoryProvider` interfaces in `internal/domain/ports/` to enforce CQRS.
    - [x] **2. Infrastructure Layer (Archive Adapter)**: Implement `ArchiveReader` using `os.File.Seek(offset, io.SeekStart)` for O(1) byte-offset reads. Write a benchmark proving low memory allocation on a 50MB file.
    - [x] **3. Application Layer (Unified Facade)**: Implement `UnifiedHistoryProvider` to stitch active/archive data, map to `HistoryViewDTO`, and strictly filter out synthetic "System Auto-Summary" messages.
- **Task:** Implement **Interactive History Browser** using Bubble Tea TUI framework:
    - [x] Create `tell-me-go browse` subcommand with TTY-aware fallback.
    - [x] Architect a non-blocking asynchronous event loop (`tea.Cmd`) to prevent disk I/O UI freezing.
    - [x] Implement Component Composition (Viewport, SearchBar) to avoid TUI 'God Objects'.
    - [x] Add basic scrolling navigation and thought visibility toggle (spacebar).
    - [x] Add full-text search across the complete conversation timeline.
    - [x] Support turn jumping and tool call expansion.
    - [x] **Asset Hydration Check**: Render placeholders (`[Image Attached: {ID}]`) to prevent binary blob panics in the TUI.
    - [x] Integrate with existing pin/unpin and rollback functionality via Command Ports.
    - [x] **Immutable Archive Boundary**: Visually disable/block mutation commands (Pin/Rollback) for messages originating from the archive.
    - [x] **Pinning Pressure Warning (Minor)**: Display a UI warning if the user pins too many active messages, preventing safe auto-summarization.
    - [x] **Live Reload (Minor)**: Optional EventBus subscription to gracefully refresh the TUI if `history.jsonl` is modified in another terminal window.
- **Reference:** [ADR-008: Bubble Tea Interactive History Browser](./docs/adr/2026-02-bubble-tea-history-browser.md)