// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockServiceConfigLoader is a mock of ConfigLoader.
type mockServiceConfigLoader struct {
	mock.Mock
}

func (m *mockServiceConfigLoader) Load(path string) (*config.Config, error) {
	args := m.Called(path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*config.Config), args.Error(1)
}

func (m *mockServiceConfigLoader) Watch(ctx context.Context, path string) (<-chan *config.Config, error) {
	args := m.Called(ctx, path)
	return args.Get(0).(<-chan *config.Config), args.Error(1)
}

// mockServiceSecurityManager is a mock of Manager.
type mockServiceSecurityManager struct {
	mock.Mock
}

func (m *mockServiceSecurityManager) IsPathSafe(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}
func (m *mockServiceSecurityManager) IsPathWritable(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}
func (m *mockServiceSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	args := m.Called(ctx, label, detail, reason, isSafe)
	return args.Bool(0), args.Error(1)
}
func (m *mockServiceSecurityManager) LogAudit(action string, args ...any) {
	m.Called(action, args)
}
func (m *mockServiceSecurityManager) TerminalLock()         { m.Called() }
func (m *mockServiceSecurityManager) TerminalUnlock()       { m.Called() }
func (m *mockServiceSecurityManager) Prompt(message string) { m.Called(message) }
func (m *mockServiceSecurityManager) Warn(message string)   { m.Called(message) }
func (m *mockServiceSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *mockServiceSecurityManager) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockServiceSecurityManager) IsCommandAllowed(command string) bool {
	return m.Called(command).Bool(0)
}
func (m *mockServiceSecurityManager) IsBypassActive() bool { return m.Called().Bool(0) }
func (m *mockServiceSecurityManager) Close() error         { return m.Called().Error(0) }

// mockServiceContainer is a mock of Container.
type mockServiceContainer struct {
	mock.Mock
}

func (m *mockServiceContainer) BuildSessionDependencies(ctx context.Context, cfg *config.Config, configPath string, newSession bool, capturer security.UserInteractor) (ports.SessionDependencies, *history.Manager, func(), error) {
	args := m.Called(ctx, cfg, configPath, newSession, capturer)
	var deps ports.SessionDependencies
	if args.Get(0) != nil {
		deps = args.Get(0).(ports.SessionDependencies)
	}
	var hManager *history.Manager
	if args.Get(1) != nil {
		hManager = args.Get(1).(*history.Manager)
	}
	return deps, hManager, args.Get(2).(func()), args.Error(3)
}

func (m *mockServiceContainer) GetAgentFactory() ports.ChatterFactory {
	args := m.Called()
	return args.Get(0).(ports.ChatterFactory)
}

func (m *mockServiceContainer) FinalizeSession(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *config.Config) error {
	args := m.Called(ctx, hManager, deps, cfg)
	return args.Error(0)
}

// mockServiceSessionDependencies is a mock of SessionDependencies.
type mockServiceSessionDependencies struct {
	mock.Mock
}

func (m *mockServiceSessionDependencies) GetGateway() llm.LLMGateway { return nil }
func (m *mockServiceSessionDependencies) GetHistoryManager() ports.HistoryManager {
	return m.Called().Get(0).(ports.HistoryManager)
}
func (m *mockServiceSessionDependencies) GetRegistry() tools.Registry { return nil }
func (m *mockServiceSessionDependencies) GetSecurityManager() security.Manager {
	return m.Called().Get(0).(security.Manager)
}
func (m *mockServiceSessionDependencies) GetEventBus() events.EventBus {
	return m.Called().Get(0).(events.EventBus)
}
func (m *mockServiceSessionDependencies) GetLogger() *slog.Logger {
	return m.Called().Get(0).(*slog.Logger)
}
func (m *mockServiceSessionDependencies) GetPaths() *persistence.Paths {
	return m.Called().Get(0).(*persistence.Paths)
}
func (m *mockServiceSessionDependencies) GetPricingOverrides() map[string]pricing.ModelPricing {
	return nil
}
func (m *mockServiceSessionDependencies) GetTracker() pricing.CostTracker { return nil }
func (m *mockServiceSessionDependencies) GetPricingData() pricing.PricingData {
	return pricing.PricingData{}
}
func (m *mockServiceSessionDependencies) GetClient() llm.LLMClient { return nil }

// mockServiceEventBus is a mock of EventBus.
type mockServiceEventBus struct {
	mock.Mock
}

func (m *mockServiceEventBus) Publish(ctx context.Context, e events.Event) error {
	return m.Called(ctx, e).Error(0)
}
func (m *mockServiceEventBus) Subscribe(sub func(context.Context, events.Event)) {
	m.Called(sub)
}
func (m *mockServiceEventBus) Shutdown(ctx context.Context) error { return m.Called(ctx).Error(0) }
func (m *mockServiceEventBus) Flush(ctx context.Context) error    { return m.Called(ctx).Error(0) }

// mockServiceAgent is a mock of Chatter.
type mockServiceAgent struct {
	mock.Mock
}

func (m *mockServiceAgent) Chat(ctx context.Context, sess *ports.Session, prompt string) error {
	return m.Called(ctx, sess, prompt).Error(0)
}
func (m *mockServiceAgent) SetLimits(ctx context.Context, maxTurns, contextWindow, historyTurns int) error {
	return m.Called(ctx, maxTurns, contextWindow, historyTurns).Error(0)
}
func (m *mockServiceAgent) SetTieredThreshold(ctx context.Context, threshold int) error {
	return m.Called(ctx, threshold).Error(0)
}
func (m *mockServiceAgent) Subscribe(handler func(context.Context, events.Event)) { m.Called(handler) }
func (m *mockServiceAgent) Shutdown(ctx context.Context) error                    { return m.Called(ctx).Error(0) }

// mockServiceCapturer is a mock of Capturer.
type mockServiceCapturer struct {
	mock.Mock
}

func (m *mockServiceCapturer) IsTTY(v any) bool {
	args := m.Called(v)
	return args.Bool(0)
}

func (m *mockServiceCapturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...orchestration.CaptureOption) (string, error) {
	args := m.Called(ctx, fs, opts)
	return args.String(0), args.Error(1)
}

func (m *mockServiceCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *mockServiceCapturer) Warn(msg string)   { m.Called(msg) }
func (m *mockServiceCapturer) Prompt(msg string) { m.Called(msg) }
func (m *mockServiceCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockServiceCapturer) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

func TestProcessMessage(t *testing.T) {
	errFileNotFound := errors.New("file not found")
	errBuild := errors.New("build error")

	tests := []struct {
		name        string
		setupMock   func(l *mockServiceConfigLoader, c *mockServiceContainer, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent) func()
		opts        ChatOptions
		wantErr     bool
		errMsg      string
		expectedErr error
	}{
		{
			name: "Success",
			opts: ChatOptions{ConfigPath: "config.yaml", Prompt: "hello"},
			setupMock: func(l *mockServiceConfigLoader, c *mockServiceContainer, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent) func() {
				cfg := &config.Config{
					Mode: "assistant",
					Providers: map[string]config.LLMProvider{
						"test": {Model: "test-model"},
					},
					SelectedProvider: "test",
				}
				l.On("Load", "config.yaml").Return(cfg, nil)

				cleanupCalled := false
				cleanup := func() { cleanupCalled = true }

				c.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(deps, &history.Manager{}, cleanup, nil)
				c.On("GetAgentFactory").Return(ports.ChatterFactory(func(ctx context.Context, sd ports.SessionDependencies, cCfg ports.ChatterConfig) (ports.Chatter, error) {
					return agent, nil
				}))
				c.On("FinalizeSession", mock.Anything, mock.Anything, deps, cfg).Return(nil)

				deps.On("GetEventBus").Return(bus)
				deps.On("GetPaths").Return(&persistence.Paths{})
				deps.On("GetHistoryManager").Return(&history.Manager{})
				deps.On("GetPricingData").Return(pricing.PricingData{})

				bus.On("Shutdown", mock.Anything).Return(nil)

				agent.On("SetLimits", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
				agent.On("SetTieredThreshold", mock.Anything, mock.Anything).Return(nil)
				agent.On("Subscribe", mock.Anything).Return()
				agent.On("Chat", mock.Anything, mock.Anything, "hello").Return(nil)
				agent.On("Shutdown", mock.Anything).Return(nil)

				cap.On("IsTTY", mock.Anything).Return(true)

				return func() {
					assert.True(t, cleanupCalled)
				}
			},
		},
		{
			name: "BuildSessionDepsError",
			opts: ChatOptions{ConfigPath: "config.yaml"},
			setupMock: func(l *mockServiceConfigLoader, c *mockServiceContainer, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent) func() {
				cfg := &config.Config{Mode: "assistant"}
				l.On("Load", "config.yaml").Return(cfg, nil)
				c.On("BuildSessionDependencies", mock.Anything, cfg, "config.yaml", false, cap).Return(nil, nil, func() {}, errBuild)
				return nil
			},
			wantErr:     true,
			errMsg:      "build error",
			expectedErr: errBuild,
		},
		{
			name: "ConfigLoadError",
			opts: ChatOptions{ConfigPath: "invalid.yaml"},
			setupMock: func(l *mockServiceConfigLoader, c *mockServiceContainer, sm *mockServiceSecurityManager, cap *mockServiceCapturer, deps *mockServiceSessionDependencies, bus *mockServiceEventBus, agent *mockServiceAgent) func() {
				l.On("Load", "invalid.yaml").Return((*config.Config)(nil), errFileNotFound)
				return nil
			},
			wantErr:     true,
			errMsg:      "invalid.yaml",
			expectedErr: errFileNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			loader := &mockServiceConfigLoader{}
			container := &mockServiceContainer{}
			sm := &mockServiceSecurityManager{}
			capturer := &mockServiceCapturer{}
			deps := &mockServiceSessionDependencies{}
			bus := &mockServiceEventBus{}
			agent := &mockServiceAgent{}

			service := NewChatService("home", "v1", io.Discard, io.Discard, sm, loader, container)

			var verify func()
			if tt.setupMock != nil {
				verify = tt.setupMock(loader, container, sm, capturer, deps, bus, agent)
			}

			err := service.ProcessMessage(ctx, tt.opts, capturer)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				if tt.expectedErr != nil {
					assert.ErrorIs(t, err, tt.expectedErr)
				}
			} else {
				assert.NoError(t, err)
			}

			if verify != nil {
				verify()
			}

			loader.AssertExpectations(t)
			container.AssertExpectations(t)
			if !tt.wantErr {
				bus.AssertExpectations(t)
				agent.AssertExpectations(t)
			}
		})
	}
}
