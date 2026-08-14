// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package ports defines the primary interfaces (ports) for the hexagonal
// architecture of tell-me-go. These interfaces establish the contracts
// between the domain core and all external adapters (infrastructure, UI,
// persistence).
//
// The registry below is the scan-driven, gate-verified membership roster of
// this package (issue #1343, ADR-064). Every exported symbol must appear in
// exactly one bucket: an interface type in exactly one family, or a
// non-interface export in Supporting. verify-ports-registry enforces the
// bijection against the live indexer.
//
// # Registry
// ## Family: Agent Lifecycle & Configuration
//   - Chatter
//   - ChatterComposer
//   - ChatExecutor
//   - ChatConfigurator
//   - ChatEventSource
//   - ChatService
//   - SessionFinalizer
//   - SessionLifecycleManager
//   - ClientFactory
//
// ## Family: Conversation Persistence
//   - HistoryManager
//   - HistoryReader
//   - HistoryWriter
//   - HistoryModifier
//   - HistoryRenderer
//   - ArchiveReader
//   - UnifiedHistoryProvider
//   - HistoryBrowser
//   - HistoryEditor
//
// ## Family: User Interaction
//   - Capturer
//   - CapturerInteractor
//   - UIRenderer
//   - ResponseRenderer
//   - StatusLogger
//   - UsageLogger
//   - ToolLogger
//   - RendererConfigurator
//   - ProgressRenderer
//   - EventSubscriber
//
// ## Family: System Diagnostics
//   - HealthChecker
//   - HealthCheckManager
//   - SystemMetricsProvider
//
// ## Family: Conversation Compression
//   - Summarizer
//
// ## Family: Persistence & State
//   - PersistenceProvider
//   - SessionProvider
//   - SessionStateProvider
//   - TaskStore
//   - TaskReader
//   - TaskWriter
//   - KVStore
//   - ListStore
//   - LogFileOpener
//   - Initializer
//   - ResourceCloser
//   - FilterableItem
//   - HistoryManagerProvider
//
// ## Family: Observability
//   - Logger
//   - TurnsLogger
//
// ## Family: Suggestions
//   - SuggestionService
//   - SuggestionProvider
//   - PromptTracker
//
// ## Supporting
//   - ChatterFactory
//   - ChatCommand
//   - ChatServiceConfig
//   - CaptureOptions
//   - CaptureOption
//   - ClientFactoryFunc
//   - Component
//   - HealthStatus
//   - ComponentReport
//   - HealthReport
//   - HistoryViewDTO
//   - HistoryRenderOptions
//   - NoOpLogger
//   - NoOpTurnsLogger
//   - ListFilter
//   - Task
//   - SessionInfo
//   - Session
//   - ChatterConfig
//   - ErrHistoryNotFound
//   - ErrEditAborted
//   - ErrTaskNotFound
//   - CompPersistence
//   - CompLLMProvider
//   - CompToolchain
//   - StatusHealthy
//   - StatusDegraded
//   - StatusUnhealthy
//   - DefaultShutdownTimeout
//   - WithSkipTTYWait
//   - WithRaw
//   - WithTUIPrompt
//   - NewSession
//
// All interfaces in this package are designed for dependency injection.
// Implementations live in internal/infrastructure/ and internal/ui/.
package ports
