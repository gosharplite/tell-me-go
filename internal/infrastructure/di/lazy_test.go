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

	cfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	callCount := 0
	simulatedErr := errors.New("llm init failed")

	clientFactory := func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
		callCount++
		return nil, simulatedErr
	}

	b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, clientFactory)

	// 1. BuildSessionDependencies should SUCCEED
	deps, _, cleanup, err := b.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
	require.NoError(t, err)
	require.NotNil(t, deps)
	defer func() { _ = cleanup(ctx) }()

	// Verify client hasn't been initialized yet
	assert.Equal(t, 0, callCount)

	// 2. GetGateway should return a non-nil proxy
	gw := deps.GetGateway()
	assert.NotNil(t, gw)
	assert.Equal(t, 0, callCount) // Getter itself doesn't trigger init anymore

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

	// 5. GetClient should also return a non-nil proxy
	concreteDeps, ok := deps.(*sessionDeps)
	require.True(t, ok)
	client := concreteDeps.GetClient()
	assert.NotNil(t, client)

	// Calling methods on client proxy should also return the same error
	err = client.RefreshAuth()
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

	cfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil, nil, nil)

	callCount := 0
	mockToolchain := new(mockToolchainFactory)
	mockToolchain.On("BuildRegistry", mock.Anything).Run(func(args mock.Arguments) {
		callCount++
	}).Return(nil, nil)
	b.toolchainFactory = mockToolchain

	// 1. BuildSessionDependencies should NOT trigger registry construction
	deps, _, cleanup, err := b.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
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

func TestLazyLLMProxy_GenerateImages(t *testing.T) {
	mockClient := new(mockExtendedClient)
	mockClient.On("GenerateImages", mock.Anything, "test-model", "test-prompt", "image/png").Return([][]byte{{0x01}}, nil)

	deps := &sessionDeps{
		clientFactory: func() (llm.ExtendedClient, error) {
			return mockClient, nil
		},
	}
	proxy := &lazyLLMProxy{deps: deps}

	images, err := proxy.GenerateImages(context.Background(), "test-model", "test-prompt", "image/png")
	assert.NoError(t, err)
	assert.Len(t, images, 1)
	mockClient.AssertExpectations(t)
}

func TestLazyLLMProxy_RefreshAuth(t *testing.T) {
	mockClient := new(mockExtendedClient)
	mockClient.On("RefreshAuth").Return(nil)

	deps := &sessionDeps{
		clientFactory: func() (llm.ExtendedClient, error) {
			return mockClient, nil
		},
	}
	proxy := &lazyLLMProxy{deps: deps}

	err := proxy.RefreshAuth()
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
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

func TestLazyLLMProxy_Generate(t *testing.T) {
	mockClient := new(mockExtendedClient)
	mockClient.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{}, &llm.Metrics{}, nil)

	deps := &sessionDeps{
		clientFactory: func() (llm.ExtendedClient, error) {
			return mockClient, nil
		},
	}
	proxy := &lazyLLMProxy{deps: deps}

	_, _, err := proxy.Generate(context.Background(), nil, nil, nil)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestLazyLLMProxy_SendChat(t *testing.T) {
	mockClient := new(mockExtendedClient)
	mockClient.On("SendChat", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{}, &llm.Metrics{}, nil)

	deps := &sessionDeps{
		clientFactory: func() (llm.ExtendedClient, error) {
			return mockClient, nil
		},
	}
	proxy := &lazyLLMProxy{deps: deps}

	_, _, err := proxy.SendChat(context.Background(), nil, nil, nil)
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestLazyLLMProxy_InitializationFailure_SendChat(t *testing.T) {
	simulatedErr := errors.New("llm init failed")
	deps := &sessionDeps{
		clientFactory: func() (llm.ExtendedClient, error) {
			return nil, simulatedErr
		},
	}
	proxy := &lazyLLMProxy{deps: deps}

	_, _, err := proxy.SendChat(context.Background(), nil, nil, nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, simulatedErr)
}

func TestLazyLLMProxy_InitializationFailure_Generate(t *testing.T) {
	simulatedErr := errors.New("llm init failed")
	deps := &sessionDeps{
		clientFactory: func() (llm.ExtendedClient, error) {
			return nil, simulatedErr
		},
	}
	proxy := &lazyLLMProxy{deps: deps}

	_, _, err := proxy.Generate(context.Background(), nil, nil, nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, simulatedErr)
}

func TestLazyLLMProxy_InitializationFailure_GenerateImages(t *testing.T) {
	simulatedErr := errors.New("llm init failed")
	deps := &sessionDeps{
		clientFactory: func() (llm.ExtendedClient, error) {
			return nil, simulatedErr
		},
	}
	proxy := &lazyLLMProxy{deps: deps}

	_, err := proxy.GenerateImages(context.Background(), "", "", "")
	assert.Error(t, err)
	assert.ErrorIs(t, err, simulatedErr)
}
