// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestBuildSessionDependencies_LazyInitialization_Proxy(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
	sm.On("SetBypassActive", mock.Anything).Return().Maybe()

	testCfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	callCount := 0
	simulatedErr := errors.New("llm init failed")

	clientFactory := ports.ClientFactoryFunc(func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		callCount++
		return nil, simulatedErr
	})

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	bcfg.ClientFactory = clientFactory
	b := NewBootstrapper(bcfg)

	// 1. BuildSessionDependencies should SUCCEED
	deps, _, cleanup, err := b.BuildSessionDependencies(ctx, testCfg, "config.yaml", false, nil)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = cleanup(ctx) }()

	// Verify client hasn't been initialized yet
	assert.Equal(t, 0, callCount)

	// 2. GetGateway should return a non-nil lazyClient
	gw := deps.GetGateway()
	assert.NotNil(t, gw)
	assert.Equal(t, 0, callCount) // Getter itself doesn't trigger init

	// 3. Calling Generate should trigger initialization and return error
	_, _, err = gw.Generate(ctx, nil, nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM provider initialization failed")
	assert.ErrorIs(t, err, simulatedErr)
	assert.Equal(t, 1, callCount)

	// 4. Subsequent calls should reuse the cached error and NOT trigger init again
	_, _, err = gw.Generate(ctx, nil, nil, nil)
	assert.Error(t, err)
	assert.Equal(t, 1, callCount)

	// 5. GetGateway should also surface the cached error via Generate
	gw2 := deps.GetGateway()
	assert.NotNil(t, gw2)

	_, _, err = gw2.Generate(ctx, nil, nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LLM provider initialization failed")
}

func TestBuildSessionDependencies_LazyRegistry(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	sm := new(mockConfigurableSecurityManager)
	setupDefaultSMExpectations(sm)
	sm.On("RegisterPolicyTools", mock.Anything, mock.Anything).Return(nil).Maybe()
	sm.On("SetBypassActive", mock.Anything).Return().Maybe()

	testCfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	bcfg := DefaultBootstrapperConfig()
	bcfg.HomeDir = tempDir
	bcfg.SM = sm
	bcfg.Version = "1.0.0"
	bcfg.Stdout = io.Discard
	bcfg.Stderr = io.Discard
	b := NewBootstrapper(bcfg)

	callCount := 0
	mockToolchain := new(mockToolchainFactory)
	mockToolchain.On("BuildRegistry", mock.Anything).Run(func(args mock.Arguments) {
		callCount++
	}).Return(nil, nil)
	mockToolchain.On("BuildHealthChecker").Return(&stubHealthChecker{})
	b.toolchainFactory = mockToolchain

	// 1. BuildSessionDependencies should NOT trigger registry construction
	deps, _, cleanup, err := b.BuildSessionDependencies(ctx, testCfg, "config.yaml", false, nil)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = cleanup(ctx) }()

	assert.Equal(t, 0, callCount)

	// 2. First call to GetRegistry should trigger it
	reg, err := deps.GetRegistry()
	assert.NoError(t, err)
	assert.Equal(t, 1, callCount)
	_ = reg

	// 3. Subsequent calls should NOT trigger it again
	_, _ = deps.GetRegistry()
	assert.Equal(t, 1, callCount)
}

type mockToolchainFactory struct {
	mock.Mock
}

func (m *mockToolchainFactory) BuildRegistry(params toolchainParams) (tools.Registry, error) {
	args := m.Called(params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(tools.Registry), args.Error(1)
}

func (m *mockToolchainFactory) BuildHealthChecker() ports.HealthChecker {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ports.HealthChecker)
}

// stubHealthChecker is a minimal ports.HealthChecker for tests.
type stubHealthChecker struct{}

func (s *stubHealthChecker) Check(ctx context.Context) (*ports.ComponentReport, error) {
	return &ports.ComponentReport{Status: ports.StatusHealthy}, nil
}

type mockExtendedClient struct {
	mock.Mock
}

func (m *mockExtendedClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	args := m.Called(ctx, input, tools, resolver)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).(*llm.Content), args.Get(1).(*llm.Metrics), args.Error(2)
}

func (m *mockExtendedClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	args := m.Called(ctx, history, tools, resolver)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).(*llm.Content), args.Get(1).(*llm.Metrics), args.Error(2)
}

func (m *mockExtendedClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	args := m.Called(ctx, model, prompt, mimeType)
	return args.Get(0).([][]byte), args.Error(1)
}

func (m *mockExtendedClient) RefreshAuth() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockExtendedClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockExtendedClient) GetModel() string {
	return m.Called().String(0)
}

func TestLazyClient_GenerateImages(t *testing.T) {
	mockClient := new(mockExtendedClient)
	mockClient.On("GenerateImages", mock.Anything, "test-model", "test-prompt", "image/png").Return([][]byte{{0x01}}, nil)

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return mockClient, nil
	})

	images, err := lc.GenerateImages(context.Background(), "test-model", "test-prompt", "image/png")
	assert.NoError(t, err)
	assert.Len(t, images, 1)
	mockClient.AssertExpectations(t)
}

func TestLazyClient_RefreshAuth(t *testing.T) {
	mockClient := new(mockExtendedClient)
	mockClient.On("RefreshAuth").Return(nil)

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return mockClient, nil
	})

	err := lc.RefreshAuth()
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestLazyClient_InitializationFailure_RefreshAuth(t *testing.T) {
	simulatedErr := errors.New("llm init failed")
	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return nil, simulatedErr
	})

	err := lc.RefreshAuth()
	assert.Error(t, err)
	assert.ErrorIs(t, err, simulatedErr)
	assert.Contains(t, err.Error(), "LLM provider initialization failed")
}

func TestSessionDeps_AdditionalGetters(t *testing.T) {
	deps := &sessionDeps{
		logger:      telemetry.NewSlogLogger(nil),
		turnsLogger: nil, // We'll just check if it's there
	}

	assert.NotNil(t, deps.GetLogger())
	// Should not panic even if turnsLogger is nil (though production always sets it)
	assert.Nil(t, deps.GetTurnsLogger())
}

func TestLazyClient_Generate(t *testing.T) {
	mockClient := new(mockExtendedClient)
	mockClient.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{}, &llm.Metrics{}, nil)

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return mockClient, nil
	})

	_, _, err := lc.Generate(context.Background(), nil, nil, nil)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestLazyClient_SendChat(t *testing.T) {
	mockClient := new(mockExtendedClient)
	mockClient.On("SendChat", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{}, &llm.Metrics{}, nil)

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return mockClient, nil
	})

	_, _, err := lc.SendChat(context.Background(), nil, nil, nil)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestLazyClient_InitializationFailure_SendChat(t *testing.T) {
	simulatedErr := errors.New("llm init failed")
	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return nil, simulatedErr
	})

	_, _, err := lc.SendChat(context.Background(), nil, nil, nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, simulatedErr)
}

func TestLazyClient_InitializationFailure_Generate(t *testing.T) {
	simulatedErr := errors.New("llm init failed")
	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return nil, simulatedErr
	})

	_, _, err := lc.Generate(context.Background(), nil, nil, nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, simulatedErr)
}

func TestLazyClient_InitializationFailure_GenerateImages(t *testing.T) {
	simulatedErr := errors.New("llm init failed")
	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return nil, simulatedErr
	})

	_, err := lc.GenerateImages(context.Background(), "", "", "")
	assert.Error(t, err)
	assert.ErrorIs(t, err, simulatedErr)
}
