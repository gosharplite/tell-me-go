// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package llm provides the infrastructure layer for LLM provider integration.
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
// Resilience (resilient_client.go):
//
//	NewResilientClient wraps any LLMClient with automatic retry on
//	transient failures, auth token refresh, and connection reset. It
//	classifies errors using the llmerr package and delegates to the
//	underlying client for normal operation.
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
