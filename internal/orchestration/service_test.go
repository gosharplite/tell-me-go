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

// mockConfigLoader is a mock of ConfigLoader.
type mockConfigLoader struct {
	mock.Mock
}

func (m *mockConfigLoader) Load(path string) (*config.Config, error) {
	args := m.Called(path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*config.Config), args.Error(1)
}

func (m *mockConfigLoader) Watch(ctx context.Context, path string) (<-chan *config.Config, error) {
	args := m.Called(ctx, path)
	return args.Get(0).(<-chan *config.Config), args.Error(1)
}

// mockSecurityManager is a mock of ISecurityManager.
type mockSecurityManager struct {
	mock.Mock
}

func (m *mockSecurityManager) IsPathSafe(path string) (string, error)     { args := m.Called(path); return args.String(0), args.Error(1) }
func (m *mockSecurityManager) IsPathWritable(path string) (string, error) { args := m.Called(path); return args.String(0), args.Error(1) }
func (m *mockSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	args := m.Called(ctx, label, detail, reason, isSafe)
	return args.Bool(0), args.Error(1)
}
func (m *mockSecurityManager) LogAudit(label1, val1, label2, val2 string) { m.Called(label1, val1, label2, val2) }
func (m *mockSecurityManager) TerminalLock()                              { m.Called() }
func (m *mockSecurityManager) TerminalUnlock()                            { m.Called() }
func (m *mockSecurityManager) Prompt(message string)                      { m.Called(message) }
func (m *mockSecurityManager) Warn(message string)                        { m.Called(message) }
func (m *mockSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *mockSecurityManager) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockSecurityManager) IsCommandAllowed(command string) bool { return m.Called(command).Bool(0) }
func (m *mockSecurityManager) IsBypassActive() bool              { return m.Called().Bool(0) }

// mockContainer is a mock of Container.
type mockContainer struct {
	mock.Mock
}

func (m *mockContainer) BuildSessionDependencies(ctx context.Context, cfg *config.Config, configPath string, newSession bool, capturer security.UserInteractor) (ports.SessionDependencies, *history.Manager, func(), error) {
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

func (m *mockContainer) GetAgentFactory() ports.ChatterFactory {
	args := m.Called()
	return args.Get(0).(ports.ChatterFactory)
}

func (m *mockContainer) FinalizeSession(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *config.Config) {
	m.Called(ctx, hManager, deps, cfg)
}

// mockSessionDependencies is a mock of SessionDependencies.
type mockSessionDependencies struct {
	mock.Mock
}

func (m *mockSessionDependencies) GetGateway() llm.LLMGateway          { return nil }
func (m *mockSessionDependencies) GetHistoryManager() ports.HistoryManager { return m.Called().Get(0).(ports.HistoryManager) }
func (m *mockSessionDependencies) GetRegistry() tools.IToolRegistry      { return nil }
func (m *mockSessionDependencies) GetSecurityManager() security.ISecurityManager {
	return m.Called().Get(0).(security.ISecurityManager)
}
func (m *mockSessionDependencies) GetEventBus() events.EventBus { return m.Called().Get(0).(events.EventBus) }
func (m *mockSessionDependencies) GetPaths() *persistence.Paths { return m.Called().Get(0).(*persistence.Paths) }
func (m *mockSessionDependencies) GetPricingOverrides() map[string]pricing.ModelPricing { return nil }
func (m *mockSessionDependencies) GetTracker() pricing.ICostTracker { return nil }
func (m *mockSessionDependencies) GetPricingData() pricing.PricingData { return pricing.PricingData{} }
func (m *mockSessionDependencies) GetClient() llm.LLMClient                      { return nil }

// mockEventBus is a mock of EventBus.
type mockEventBus struct {
	mock.Mock
}

func (m *mockEventBus) Publish(ctx context.Context, e events.Event) error { return m.Called(ctx, e).Error(0) }
func (m *mockEventBus) Subscribe(sub func(events.Event)) {
	m.Called(sub)
}
func (m *mockEventBus) Shutdown(ctx context.Context) error { return m.Called(ctx).Error(0) }
func (m *mockEventBus) Flush(ctx context.Context) error    { return m.Called(ctx).Error(0) }

// mockAgent is a mock of Chatter.
type mockAgent struct {
	mock.Mock
}

func (m *mockAgent) Chat(ctx context.Context, sess *ports.Session, prompt string) error {
	return m.Called(ctx, sess, prompt).Error(0)
}
func (m *mockAgent) SetLimits(ctx context.Context, maxTurns, contextWindow, historyTurns int) error {
	return m.Called(ctx, maxTurns, contextWindow, historyTurns).Error(0)
}
func (m *mockAgent) SetTieredThreshold(ctx context.Context, threshold int) error {
	return m.Called(ctx, threshold).Error(0)
}
func (m *mockAgent) Subscribe(handler func(events.Event)) { m.Called(handler) }
func (m *mockAgent) Shutdown(ctx context.Context) error   { return m.Called(ctx).Error(0) }

// mockCapturer is a mock of Capturer.
type mockCapturer struct {
	mock.Mock
}

func (m *mockCapturer) IsTTY(v any) bool {
	args := m.Called(v)
	return args.Bool(0)
}

func (m *mockCapturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...orchestration.CaptureOption) (string, error) {
	args := m.Called(ctx, fs, opts)
	return args.String(0), args.Error(1)
}

func (m *mockCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *mockCapturer) Warn(msg string)                      { m.Called(msg) }
func (m *mockCapturer) Prompt(msg string)                    { m.Called(msg) }
func (m *mockCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockCapturer) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}

func TestProcessMessage_Success(t *testing.T) {
	ctx := context.Background()
	loader := &mockConfigLoader{}
	container := &mockContainer{}
	sm := &mockSecurityManager{}
	capturer := &mockCapturer{}
	deps := &mockSessionDependencies{}
	bus := &mockEventBus{}
	agent := &mockAgent{}

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
	loader := &mockConfigLoader{}
	container := &mockContainer{}
	sm := &mockSecurityManager{}
	capturer := &mockCapturer{}

	service := NewChatService("home", "v1", io.Discard, io.Discard, sm, loader, container)

	cfg := &config.Config{Mode: "assistant"}
	loader.On("Load", "config.yaml").Return(cfg, nil)

	container.On("BuildSessionDependencies", ctx, cfg, "config.yaml", false, capturer).Return(nil, nil, func() {}, errors.New("build error"))

	opts := ChatOptions{ConfigPath: "config.yaml"}
	err := service.ProcessMessage(ctx, opts, capturer)

	assert.Error(t, err)
	assert.Equal(t, "build error", err.Error())
}
