// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package llm provides the infrastructure layer for LLM provider integration.
//
// # Resilience Architecture (Two-Layer)
//
// Error recovery is split across two layers with distinct responsibilities:
//
//	Layer 1 — resilientClient (per-provider, resilient_client.go):
//	  Wraps a single provider client. On SendChat failure:
//	    1. Classifies the raw error into a domain sentinel via llmerr.Classify
//	       (ErrAuth / ErrTransient / ErrRateLimit / ErrTerminal).
//	    2. On ErrAuth (first attempt only): refreshes the auth token and retries
//	       once. If the refresh succeeds, the retry proceeds; otherwise it returns
//	       ErrAuth to the caller.
//	    3. On transient errors or final attempt: resets the HTTP connection pool
//	       to evict poisoned keep-alive connections.
//	    4. Returns the classified domain sentinel — it does NOT perform
//	       exponential-backoff retry loops. That responsibility belongs to the
//	       TurnEngine's RecoveryStep (see internal/agent/orchestrator).
//
//	  Max attempts: 2 (initial + one auth-refresh retry).
//
//	Layer 2 — FailoverGateway (cross-provider, failover.go):
//	  Iterates through an ordered list of resilientClient-wrapped providers.
//	  On Generate:
//	    - Success: returns immediately, annotating metrics with the provider name.
//	    - ErrTransient / ErrRateLimit: tries the next provider in the chain.
//	    - ErrAuth / ErrTerminal / unrecognized: aborts the chain immediately —
//	      these are not retryable by switching providers.
//	    - All providers exhausted: wraps the last error as ErrTerminal.
//
//	  Non-Generate methods (SendChat, GenerateImages, RefreshAuth) delegate to
//	  the primary (first) provider only.
//
//	  Key invariant: every client in a FailoverGateway MUST be wrapped in
//	  resilientClient. This is enforced by newFailoverChain in factory.go.
//
//	Error flow summary:
//	  Raw HTTP/gRPC error
//	    → resilientClient.wrapError → llmerr.Classify → domain sentinel
//	      → FailoverGateway reads sentinel, decides: try-next or abort
//	        → TurnEngine.RecoveryStep reads sentinel, decides: retry-with-backoff or fail
//
// # Components
//
// Factory (factory.go):
//
//	newClient is the central factory that inspects configuration and
//	instantiates the appropriate provider client (OpenAI, Anthropic, or
//	Google Gemini). It handles authentication, timeout resolution,
//	thinking budget, and wraps the result in a resilient client.
//
//	newFailoverChain constructs a FailoverGateway from the configured
//	failover provider order. Each provider in the chain is independently
//	wrapped in a resilientClient.
//
// Health (provider_health.go):
//
//	NewLLMProviderHealthChecker performs connectivity and authentication
//	checks against the configured LLM provider. It implements
//	ports.HealthChecker and reports per-component status.
//
// Summarization (summarizer.go):
//
//	NewSummarizer implements ports.Summarizer using the LLM gateway to
//	compress conversation history into a structured Markdown summary.
//	It strips binary data before sending to avoid INVALID_ARGUMENT errors.
//
// # Sub-Packages
//
//   - anthropic: Anthropic Messages API client
//   - openai: OpenAI Chat Completions API client (also supports DeepSeek)
//   - gemini: Google Gemini / Vertex AI client
//   - llmerr: Unified error classification across all providers
package llm
