# tell-me-go Project Roadmap

This document outlines the strategic evolution of `tell-me-go`. Our primary goal is to provide a unified, provider-agnostic interface for high-performance reasoning models.

- **Strategic Strategy:** [ADR-001: Hybrid LLM Infrastructure Strategy](./docs/adr/2024-05-multi-llm-provider-strategy.md)
- **Technical Specification:** [Multi-Provider Implementation Plan](./docs/sop/technical/multi_provider_implementation.md)
- **Design Standards:** [ADR Management SOP](./docs/sop/standards/adr_standards.md)

## Phase 1: Foundation & Governance (Completed)
- [x] Documentation & ADR Initialization
- [x] ADR-001: Hybrid Infrastructure Strategy
- [x] Refactor Domain `llm.Part`: Migrate `Thought` from `bool` to `string`.
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
- [x] History Log Compaction (ADR-001): Implemented `jsonlStore` patches and persistent archiving to solve memory/OOM bloat on long-running sessions.

## Phase 5: Advanced Capability (Completed)
- [x] Silent Observability: Implement conditional OpenTelemetry exporter integration in `main.go` using `TELL_ME_TRACE_ENDPOINT`.

## Phase 6: Domain Decomposition & Scalability (New)
- **Goal:** Address Single Responsibility Principle (SRP) violations and decompose God Objects.
- **Task:** Investigate the `Chatter` domain service. It currently accepts a massive `ChatterParams` struct with 13 dependencies. 
- **Task:** Evaluate splitting `Chatter` into distinct pipeline stages (e.g., `ContextBuilder`, `ExecutionEngine`, `ResponseProcessor`) or applying the Facade Pattern.
- **Task:** Continuous dependency injection audit to ensure the composition root (`container.go`) remains clean.

---
*Note: This roadmap is subject to change based on the evolution of LLM APIs and project requirements.*
