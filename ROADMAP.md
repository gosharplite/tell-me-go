# tell-me-go Project Roadmap

This document outlines the strategic evolution of `tell-me-go`. Our primary goal is to provide a unified, provider-agnostic interface for high-performance reasoning models.

## 📍 Current Focus & Key Documentation
We are currently executing **Phase 1: Foundation & Governance**.

- **Strategic Strategy:** [ADR-001: Hybrid LLM Infrastructure Strategy](./docs/adr/2024-05-multi-llm-provider-strategy.md)
- **Technical Specification:** [Multi-Provider Implementation Plan](./docs/sop/technical/multi_provider_implementation.md)
- **Design Standards:** [ADR Management SOP](./docs/sop/standards/adr_standards.md)

## Phase 1: Foundation & Governance (In Progress)
- [x] Documentation & ADR Initialization
- [x] ADR-001: Hybrid Infrastructure Strategy
- [ ] Configuration Schema Expansion (Infrastructure Scaffolding):
    - [ ] Implement nested `Providers` registry in `Config` struct.
    - [ ] Add support for Environment Variable expansion in YAML (e.g., `${API_KEY}`).
    - [ ] Implement `Provider` selector (google, openai, deepseek).
- [ ] Refactor Domain `llm.Part`: Migrate `Thought` from `bool` to `string` (Reasoning Content support for DeepSeek and Claude Opus 4.6 Thinking blocks).

## Phase 2: OpenAI-Compatible Infrastructure
- [ ] Implement internal/infrastructure/llm/openai:
    - [ ] Manual HTTP transport for OpenAI v1 Chat Completion.
    - [ ] Specific mapping for gpt-5.2 (reasoning_tokens) and deepseek-reasoner (reasoning_content).
- [ ] Standardize ResilientClient to support custom HTTP headers and bearer tokens.

## Phase 2.5: Anthropic (Claude) Infrastructure
- [ ] Implement internal/infrastructure/llm/anthropic:
    - [ ] Manual HTTP transport for Anthropic Messages API (/v1/messages).
    - [ ] Specific mapping for 'type: thinking' content blocks to domain 'Thought' string.
    - [ ] Support for 'x-api-key' and 'anthropic-version' headers.

## Phase 3: Orchestration & Telemetry
- [ ] Dynamic Provider Registry (Factory Pattern)
- [ ] Multi-provider Token & Cost tracking
- [ ] Config-driven provider switching

## Phase 4: Release & Optimization
- [ ] Performance benchmarking across providers
- [ ] Security auditing for new endpoints
- [ ] Public release of multi-provider support

---
*Note: This roadmap is subject to change based on the evolution of LLM APIs and project requirements.*
