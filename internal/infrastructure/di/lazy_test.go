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
