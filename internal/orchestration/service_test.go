// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"flag"
	"io"
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

// MockConfigLoader is a mock of ConfigLoader.
type MockConfigLoader struct {
	mock.Mock
}

func (m *MockConfigLoader) Load(path string) (*config.Config, error) {
	args := m.Called(path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*config.Config), args.Error(1)
}

func (m *MockConfigLoader) Watch(ctx context.Context, path string) (<-chan *config.Config, error) {
	args := m.Called(ctx, path)
	return args.Get(0).(<-chan *config.Config), args.Error(1)
}

// MockSecurityManager is a mock of ISecurityManager.
type MockSecurityManager struct {
	mock.Mock
}

func (m *MockSecurityManager) IsPathSafe(path string) (string, error)     { args := m.Called(path); return args.String(0), args.Error(1) }
func (m *MockSecurityManager) IsPathWritable(path string) (string, error) { args := m.Called(path); return args.String(0), args.Error(1) }
func (m *MockSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	args := m.Called(ctx, label, detail, reason, isSafe)
	return args.Bool(0), args.Error(1)
}
func (m *MockSecurityManager) LogAudit(label1, val1, label2, val2 string) { m.Called(label1, val1, label2, val2) }
func (m *MockSecurityManager) TerminalLock()                              { m.Called() }
func (m *MockSecurityManager) TerminalUnlock()                            { m.Called() }
func (m *MockSecurityManager) Prompt(message string)                      { m.Called(message) }
func (m *MockSecurityManager) Warn(message string)                        { m.Called(message) }
func (m *MockSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *MockSecurityManager) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *MockSecurityManager) IsCommandAllowed(command string) bool { return m.Called(command).Bool(0) }
func (m *MockSecurityManager) IsBypassActive() bool              { return m.Called().Bool(0) }

// MockContainer is a mock of Container.
type MockContainer struct {
	mock.Mock
}

func (m *MockContainer) BuildSessionDependencies(ctx context.Context, cfg *config.Config, configPath string, newSession bool, capturer security.UserInteractor) (ports.SessionDependencies, *history.Manager, func(), error) {
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

func (m *MockContainer) GetAgentFactory() ports.ChatterFactory {
	args := m.Called()
	return args.Get(0).(ports.ChatterFactory)
}

func (m *MockContainer) FinalizeSession(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *config.Config) {
	m.Called(ctx, hManager, deps, cfg)
}

// MockSessionDependencies is a mock of SessionDependencies.
type MockSessionDependencies struct {
	mock.Mock
}

func (m *MockSessionDependencies) GetGateway() llm.LLMGateway          { return nil }
func (m *MockSessionDependencies) GetHistoryManager() ports.HistoryManager { return m.Called().Get(0).(ports.HistoryManager) }
func (m *MockSessionDependencies) GetRegistry() tools.IToolRegistry      { return nil }
func (m *MockSessionDependencies) GetSecurityManager() security.ISecurityManager {
	return m.Called().Get(0).(security.ISecurityManager)
}
func (m *MockSessionDependencies) GetEventBus() events.EventBus { return m.Called().Get(0).(events.EventBus) }
func (m *MockSessionDependencies) GetPaths() *persistence.Paths { return m.Called().Get(0).(*persistence.Paths) }
func (m *MockSessionDependencies) GetPricingOverrides() map[string]pricing.ModelPricing { return nil }
func (m *MockSessionDependencies) GetTracker() pricing.ICostTracker { return nil }
func (m *MockSessionDependencies) GetPricingData() pricing.PricingData { return pricing.PricingData{} }
func (m *MockSessionDependencies) GetClient() llm.LLMClient                      { return nil }

// MockEventBus is a mock of EventBus.
type MockEventBus struct {
	mock.Mock
}

func (m *MockEventBus) Publish(ctx context.Context, e events.Event) error { return m.Called(ctx, e).Error(0) }
func (m *MockEventBus) Subscribe(sub func(events.Event)) {
	m.Called(sub)
}
func (m *MockEventBus) Shutdown(ctx context.Context) error { return m.Called(ctx).Error(0) }
func (m *MockEventBus) Flush(ctx context.Context) error    { return m.Called(ctx).Error(0) }

// MockAgent is a mock of Chatter.
type MockAgent struct {
	mock.Mock
}

func (m *MockAgent) Chat(ctx context.Context, sess *ports.Session, prompt string) error {
	return m.Called(ctx, sess, prompt).Error(0)
}
func (m *MockAgent) SetLimits(ctx context.Context, maxTurns, contextWindow, historyTurns int) error {
	return m.Called(ctx, maxTurns, contextWindow, historyTurns).Error(0)
}
func (m *MockAgent) SetTieredThreshold(ctx context.Context, threshold int) error {
	return m.Called(ctx, threshold).Error(0)
}
func (m *MockAgent) Subscribe(handler func(events.Event)) { m.Called(handler) }
func (m *MockAgent) Shutdown(ctx context.Context) error   { return m.Called(ctx).Error(0) }

// MockCapturer is a mock of Capturer.
type MockCapturer struct {
	mock.Mock
}

func (m *MockCapturer) IsTTY(v any) bool {
	args := m.Called(v)
	return args.Bool(0)
}

func (m *MockCapturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...orchestration.CaptureOption) (string, error) {
	args := m.Called(ctx, fs, opts)
	return args.String(0), args.Error(1)
}

func (m *MockCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *MockCapturer) Warn(msg string)                      { m.Called(msg) }
func (m *MockCapturer) Prompt(msg string)                    { m.Called(msg) }
func (m *MockCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *MockCapturer) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

func TestProcessMessage_Success(t *testing.T) {
	ctx := context.Background()
	loader := &MockConfigLoader{}
	container := &MockContainer{}
	sm := &MockSecurityManager{}
	capturer := &MockCapturer{}
	deps := &MockSessionDependencies{}
	bus := &MockEventBus{}
	agent := &MockAgent{}

	service := NewChatService("home", "v1", io.Discard, io.Discard, sm, loader, container)

	cfg := &config.Config{
		Mode: "assistant",
		Providers: map[string]config.LLMProvider{
			"test": {Model: "test-model"},
		},
		SelectedProvider: "test",
	}

	loader.On("Load", "config.yaml").Return(cfg, nil)

	cleanupCalled := false
	cleanup := func() { cleanupCalled = true }

	container.On("BuildSessionDependencies", ctx, cfg, "config.yaml", false, capturer).Return(deps, &history.Manager{}, cleanup, nil)
	container.On("GetAgentFactory").Return(ports.ChatterFactory(func(ctx context.Context, sd ports.SessionDependencies, cCfg ports.ChatterConfig) (ports.Chatter, error) {
		return agent, nil
	}))
	container.On("FinalizeSession", ctx, mock.Anything, deps, cfg).Return()

	deps.On("GetEventBus").Return(bus)
	deps.On("GetPaths").Return(&persistence.Paths{})
	deps.On("GetHistoryManager").Return(&history.Manager{})
	deps.On("GetPricingData").Return(pricing.PricingData{})

	bus.On("Shutdown", ctx).Return(nil)

	agent.On("SetLimits", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	agent.On("SetTieredThreshold", ctx, mock.Anything).Return(nil)
	agent.On("Subscribe", mock.Anything).Return()
	agent.On("Chat", ctx, mock.Anything, "hello").Return(nil)
	agent.On("Shutdown", ctx).Return(nil)

	capturer.On("IsTTY", mock.Anything).Return(true)

	opts := ChatOptions{ConfigPath: "config.yaml", Prompt: "hello"}
	err := service.ProcessMessage(ctx, opts, capturer)

	assert.NoError(t, err)
	assert.True(t, cleanupCalled)
	loader.AssertExpectations(t)
	container.AssertExpectations(t)
	bus.AssertExpectations(t)
	agent.AssertExpectations(t)
}

func TestProcessMessage_BuildSessionDepsError(t *testing.T) {
	ctx := context.Background()
	loader := &MockConfigLoader{}
	container := &MockContainer{}
	sm := &MockSecurityManager{}
	capturer := &MockCapturer{}

	service := NewChatService("home", "v1", io.Discard, io.Discard, sm, loader, container)

	cfg := &config.Config{Mode: "assistant"}
	loader.On("Load", "config.yaml").Return(cfg, nil)

	container.On("BuildSessionDependencies", ctx, cfg, "config.yaml", false, capturer).Return(nil, nil, func() {}, errors.New("build error"))

	opts := ChatOptions{ConfigPath: "config.yaml"}
	err := service.ProcessMessage(ctx, opts, capturer)

	assert.Error(t, err)
	assert.Equal(t, "build error", err.Error())
}
