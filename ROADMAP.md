# tell-me-go Project Roadmap

This document outlines the strategic evolution of `tell-me-go`. Our primary goal is to provide a unified, provider-agnostic interface for high-performance reasoning models.

## Phase 1: Foundation & Governance (In Progress)
- [x] Documentation & ADR Initialization
- [x] ADR-001: Hybrid Infrastructure Strategy
- [ ] Unified Domain Thinking Types (`llm.Thought`)
- [ ] Configuration Schema Expansion (OpenAI/DeepSeek support)

## Phase 2: OpenAI-Compatible Infrastructure
- [ ] Manual HTTP Client implementation for OpenAI v1 standard
- [ ] Model-specific parsing for `gpt-5.2` and `deepseek-reasoner`
- [ ] Resilience & Retry logic parity

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
