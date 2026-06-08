// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clitest

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockBootstrapper is a hand-rolled test double for cli.Bootstrapper.
// Override function fields to script behaviour. All methods record
// invocation counts and names accessible via Snapshot().
type MockBootstrapper struct {
	mu                              sync.Mutex
	calledBuildSessionDependencies  int
	calledFinalizeSession           int
	calledGetHistoryManager         int
	calledGetUnifiedHistoryProvider int
	calledGetSuggestionService      int
	calledGetAgentFactory           int
	calledGetUIRenderer             int
	calledGetHistoryRenderer        int
	calledGetHistoryBrowser         int
	calledGetChatService            int
	calledMethods                   []string

	// Function fields — set before test to script behaviour.
	BuildSessionDependenciesFunc  func(ctx context.Context, cfg *domain_config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error)
	FinalizeSessionFunc           func(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionFinalizer, cfg *domain_config.Config) error
	GetHistoryManagerFunc         func(ctx context.Context, cfg *domain_config.Config) (ports.HistoryManager, error)
	GetUnifiedHistoryProviderFunc func(ctx context.Context, cfg *domain_config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error)
	GetSuggestionServiceFunc      func(ctx context.Context, recentHistory []string) (ports.SuggestionService, error)
	GetAgentFactoryFunc           func() ports.ChatterFactory
	GetUIRendererFunc             func() ports.UIRenderer
	GetHistoryRendererFunc        func() ports.HistoryRenderer
	GetHistoryBrowserFunc         func() ports.HistoryBrowser
	GetChatServiceFunc            func() agent.ChatService
}

// BootstrapperSnapshot holds a race-safe copy of mock call counts and
// ordered method names.
type BootstrapperSnapshot struct {
	BuildSessionDependencies, FinalizeSession, GetHistoryManager int
	GetUnifiedHistoryProvider, GetSuggestionService              int
	GetAgentFactory, GetUIRenderer, GetHistoryRenderer           int
	GetHistoryBrowser, GetChatService                            int
	Methods                                                      []string
}

// Snapshot returns a race-safe copy of invocation counts and ordered
// method names. The returned Methods slice is a defensive copy safe
// for inspection without holding the mutex.
func (m *MockBootstrapper) Snapshot() BootstrapperSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	methods := make([]string, len(m.calledMethods))
	copy(methods, m.calledMethods)
	return BootstrapperSnapshot{
		BuildSessionDependencies:  m.calledBuildSessionDependencies,
		FinalizeSession:           m.calledFinalizeSession,
		GetHistoryManager:         m.calledGetHistoryManager,
		GetUnifiedHistoryProvider: m.calledGetUnifiedHistoryProvider,
		GetSuggestionService:      m.calledGetSuggestionService,
		GetAgentFactory:           m.calledGetAgentFactory,
		GetUIRenderer:             m.calledGetUIRenderer,
		GetHistoryRenderer:        m.calledGetHistoryRenderer,
		GetHistoryBrowser:         m.calledGetHistoryBrowser,
		GetChatService:            m.calledGetChatService,
		Methods:                   methods,
	}
}

// BuildSessionDependencies builds the core session dependencies.
// When BuildSessionDependenciesFunc is nil the default returns
// (nil, nil, func(context.Context) error { return nil }, nil).
func (m *MockBootstrapper) BuildSessionDependencies(ctx context.Context, cfg *domain_config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.ChatterComposer, ports.HistoryManager, func(context.Context) error, error) {
	m.mu.Lock()
	m.calledBuildSessionDependencies++
	m.calledMethods = append(m.calledMethods, "BuildSessionDependencies")
	m.mu.Unlock()

	if m.BuildSessionDependenciesFunc != nil {
		return m.BuildSessionDependenciesFunc(ctx, cfg, configPath, newSession, capturer)
	}
	return nil, nil, func(context.Context) error { return nil }, nil
}

// FinalizeSession finalizes the session state.
// When FinalizeSessionFunc is nil the default returns nil.
func (m *MockBootstrapper) FinalizeSession(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionFinalizer, cfg *domain_config.Config) error {
	m.mu.Lock()
	m.calledFinalizeSession++
	m.calledMethods = append(m.calledMethods, "FinalizeSession")
	m.mu.Unlock()

	if m.FinalizeSessionFunc != nil {
		return m.FinalizeSessionFunc(ctx, hManager, deps, cfg)
	}
	return nil
}

// GetHistoryManager loads the history manager for a given configuration.
// When GetHistoryManagerFunc is nil the default returns (nil, nil).
func (m *MockBootstrapper) GetHistoryManager(ctx context.Context, cfg *domain_config.Config) (ports.HistoryManager, error) {
	m.mu.Lock()
	m.calledGetHistoryManager++
	m.calledMethods = append(m.calledMethods, "GetHistoryManager")
	m.mu.Unlock()

	if m.GetHistoryManagerFunc != nil {
		return m.GetHistoryManagerFunc(ctx, cfg)
	}
	return nil, nil
}

// GetUnifiedHistoryProvider assembles the read-model for the history browser.
// When GetUnifiedHistoryProviderFunc is nil the default returns (nil, nil).
func (m *MockBootstrapper) GetUnifiedHistoryProvider(ctx context.Context, cfg *domain_config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error) {
	m.mu.Lock()
	m.calledGetUnifiedHistoryProvider++
	m.calledMethods = append(m.calledMethods, "GetUnifiedHistoryProvider")
	m.mu.Unlock()

	if m.GetUnifiedHistoryProviderFunc != nil {
		return m.GetUnifiedHistoryProviderFunc(ctx, cfg, hManager)
	}
	return nil, nil
}

// GetSuggestionService initializes and returns the suggestion service.
// When GetSuggestionServiceFunc is nil the default returns (nil, nil).
func (m *MockBootstrapper) GetSuggestionService(ctx context.Context, recentHistory []string) (ports.SuggestionService, error) {
	m.mu.Lock()
	m.calledGetSuggestionService++
	m.calledMethods = append(m.calledMethods, "GetSuggestionService")
	m.mu.Unlock()

	if m.GetSuggestionServiceFunc != nil {
		return m.GetSuggestionServiceFunc(ctx, recentHistory)
	}
	return nil, nil
}

// GetAgentFactory returns the chatter factory.
// When GetAgentFactoryFunc is nil the default returns nil.
func (m *MockBootstrapper) GetAgentFactory() ports.ChatterFactory {
	m.mu.Lock()
	m.calledGetAgentFactory++
	m.calledMethods = append(m.calledMethods, "GetAgentFactory")
	m.mu.Unlock()

	if m.GetAgentFactoryFunc != nil {
		return m.GetAgentFactoryFunc()
	}
	return nil
}

// GetUIRenderer returns the UI renderer.
// When GetUIRendererFunc is nil the default returns nil.
func (m *MockBootstrapper) GetUIRenderer() ports.UIRenderer {
	m.mu.Lock()
	m.calledGetUIRenderer++
	m.calledMethods = append(m.calledMethods, "GetUIRenderer")
	m.mu.Unlock()

	if m.GetUIRendererFunc != nil {
		return m.GetUIRendererFunc()
	}
	return nil
}

// GetHistoryRenderer returns the history renderer.
// When GetHistoryRendererFunc is nil the default returns nil.
func (m *MockBootstrapper) GetHistoryRenderer() ports.HistoryRenderer {
	m.mu.Lock()
	m.calledGetHistoryRenderer++
	m.calledMethods = append(m.calledMethods, "GetHistoryRenderer")
	m.mu.Unlock()

	if m.GetHistoryRendererFunc != nil {
		return m.GetHistoryRendererFunc()
	}
	return nil
}

// GetHistoryBrowser returns the history browser.
// When GetHistoryBrowserFunc is nil the default returns nil.
func (m *MockBootstrapper) GetHistoryBrowser() ports.HistoryBrowser {
	m.mu.Lock()
	m.calledGetHistoryBrowser++
	m.calledMethods = append(m.calledMethods, "GetHistoryBrowser")
	m.mu.Unlock()

	if m.GetHistoryBrowserFunc != nil {
		return m.GetHistoryBrowserFunc()
	}
	return nil
}

// GetChatService returns the chat service.
// When GetChatServiceFunc is nil the default returns nil.
func (m *MockBootstrapper) GetChatService() agent.ChatService {
	m.mu.Lock()
	m.calledGetChatService++
	m.calledMethods = append(m.calledMethods, "GetChatService")
	m.mu.Unlock()

	if m.GetChatServiceFunc != nil {
		return m.GetChatServiceFunc()
	}
	return nil
}
