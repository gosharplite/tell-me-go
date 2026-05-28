// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package ports defines the primary interfaces (ports) for the hexagonal
// architecture of tell-me-go. These interfaces establish the contracts
// between the domain core and all external adapters (infrastructure, UI,
// persistence).
//
// # Interface Families
//
// Agent Lifecycle & Configuration:
//   - Chatter / ChatterFactory / ChatExecutor / ChatConfigurator / ChatEventSource
//   - SessionConfig / SessionDependencies / Session
//
// Conversation Persistence:
//   - HistoryManager / HistoryReader / HistoryWriter / HistoryModifier
//   - HistoryRenderer / HistoryRenderOptions / HistoryViewDTO
//   - ArchiveReader / UnifiedHistoryProvider
//
// User Interaction:
//   - Capturer / CaptureOptions / CaptureOption
//   - UIRenderer / ResponseRenderer / StatusLogger / UsageLogger / ToolLogger
//   - RendererConfigurator
//
// System Diagnostics:
//   - HealthChecker / HealthCheckManager / HealthReport / HealthStatus / Component / ComponentReport
//   - SystemMetricsProvider
//
// Conversation Compression:
//   - Summarizer
//
// Context Pipeline:
//   - ContextTransformer / ContextRequest / ContextMetadata
//   - PruningPolicy / ResultStrategy
//
// Persistence & State:
//   - PersistenceProvider / SessionProvider / SessionStateProvider
//   - TaskStore / TaskReader / TaskWriter / Task
//   - KVStore / ListStore / ListFilter
//   - Initializer / ResourceCloser
//
// Observability:
//   - Logger / TurnsLogger
//
// Suggestions:
//   - SuggestionService / SuggestionProvider / PromptTracker
//
// Infrastructure Wiring:
//   - LLMDependencyProvider / PersistenceDependencyProvider / InfrastructureDependencyProvider
//   - HistoryManagerProvider / SuggestionProvider
//
// All interfaces in this package are designed for dependency injection.
// Implementations live in internal/infrastructure/ and internal/ui/.
package ports
