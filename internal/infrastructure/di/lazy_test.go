// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSessionDependencies_LazyInitialization_Proxy(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	sm := new(mockConfigurableSecurityManager)

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
	mockToolchain := &mockToolchainFactory{
		BuildRegistryFunc: func(params toolchainParams) (tools.Registry, error) {
			callCount++
			return nil, nil
		},
		BuildHealthCheckerFunc: func() ports.HealthChecker {
			return &stubHealthChecker{}
		},
	}
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

func TestLazyRegistry_ConcurrentInit_ErrorCached(t *testing.T) {
	var initCount atomic.Int32
	simulatedErr := errors.New("registry init failed")

	lr := newLazyRegistry(func() (tools.Registry, error) {
		initCount.Add(1)
		return nil, simulatedErr
	}, &ports.NoOpLogger{})

	const numGoroutines = 10
	var wg sync.WaitGroup
	regs := make([]tools.Registry, numGoroutines)
	errs := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			regs[idx], errs[idx] = lr.get()
		}(i)
	}
	wg.Wait()

	// Factory must be called exactly once
	assert.Equal(t, int32(1), initCount.Load())

	for i := 0; i < numGoroutines; i++ {
		require.Error(t, errs[i], "goroutine %d should have received an error", i)
		assert.ErrorIs(t, errs[i], simulatedErr,
			"goroutine %d error should wrap the original init error", i)
		assert.Nil(t, regs[i], "goroutine %d should have received nil registry", i)
	}
}

// mockToolchainFactory is a hand-rolled test double for toolchainFactory.
type mockToolchainFactory struct {
	BuildRegistryFunc      func(params toolchainParams) (tools.Registry, error)
	BuildHealthCheckerFunc func() ports.HealthChecker

	buildRegistryCalls      int
	buildHealthCheckerCalls int
}

func (m *mockToolchainFactory) BuildRegistry(params toolchainParams) (tools.Registry, error) {
	m.buildRegistryCalls++
	if m.BuildRegistryFunc != nil {
		return m.BuildRegistryFunc(params)
	}
	return nil, nil
}

func (m *mockToolchainFactory) BuildHealthChecker() ports.HealthChecker {
	m.buildHealthCheckerCalls++
	if m.BuildHealthCheckerFunc != nil {
		return m.BuildHealthCheckerFunc()
	}
	return nil
}

// stubHealthChecker is a minimal ports.HealthChecker for tests.
type stubHealthChecker struct{}

func (s *stubHealthChecker) Check(ctx context.Context) (*ports.ComponentReport, error) {
	return &ports.ComponentReport{Status: ports.StatusHealthy}, nil
}

// mockExtendedClient is a hand-rolled test double for llm.ExtendedClient.
// When a Func field is nil, the method returns its natural zero value.
type mockExtendedClient struct {
	GenerateFunc       func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	SendChatFunc       func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	GenerateImagesFunc func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error)
	RefreshAuthFunc    func() error
	CloseFunc          func() error
	GetModelFunc       func() string

	generateCalls       int
	sendChatCalls       int
	generateImagesCalls int
	refreshAuthCalls    int
	closeCalls          int
	getModelCalls       int
}

func (m *mockExtendedClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.generateCalls++
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, input, tools, resolver)
	}
	return &llm.Content{}, &llm.Metrics{}, nil
}

func (m *mockExtendedClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.sendChatCalls++
	if m.SendChatFunc != nil {
		return m.SendChatFunc(ctx, history, tools, resolver)
	}
	return &llm.Content{}, &llm.Metrics{}, nil
}

func (m *mockExtendedClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	m.generateImagesCalls++
	if m.GenerateImagesFunc != nil {
		return m.GenerateImagesFunc(ctx, model, prompt, mimeType)
	}
	return nil, nil
}

func (m *mockExtendedClient) RefreshAuth() error {
	m.refreshAuthCalls++
	if m.RefreshAuthFunc != nil {
		return m.RefreshAuthFunc()
	}
	return nil
}

func (m *mockExtendedClient) Close() error {
	m.closeCalls++
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *mockExtendedClient) GetModel() string {
	m.getModelCalls++
	if m.GetModelFunc != nil {
		return m.GetModelFunc()
	}
	return ""
}

func TestLazyClient_GenerateImages(t *testing.T) {
	mockClient := &mockExtendedClient{
		GenerateImagesFunc: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
			return [][]byte{{0x01}}, nil
		},
	}

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return mockClient, nil
	})

	images, err := lc.GenerateImages(context.Background(), "test-model", "test-prompt", "image/png")
	assert.NoError(t, err)
	assert.Len(t, images, 1)
	assert.Equal(t, 1, mockClient.generateImagesCalls)
}

func TestLazyClient_RefreshAuth(t *testing.T) {
	mockClient := &mockExtendedClient{
		RefreshAuthFunc: func() error { return nil },
	}

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return mockClient, nil
	})

	err := lc.RefreshAuth()
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.refreshAuthCalls)
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
		infraProvider: infraProvider{
			logger:      telemetry.NewSlogLogger(nil),
			turnsLogger: nil,
		},
	}

	assert.NotNil(t, deps.GetLogger())
	// Should not panic even if turnsLogger is nil (though production always sets it)
	assert.Nil(t, deps.GetTurnsLogger())
}

func TestLazyClient_Generate(t *testing.T) {
	mockClient := &mockExtendedClient{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{}, &llm.Metrics{}, nil
		},
	}

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return mockClient, nil
	})

	_, _, err := lc.Generate(context.Background(), nil, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.generateCalls)
}

func TestLazyClient_SendChat(t *testing.T) {
	mockClient := &mockExtendedClient{
		SendChatFunc: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{}, &llm.Metrics{}, nil
		},
	}

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		return mockClient, nil
	})

	_, _, err := lc.SendChat(context.Background(), nil, nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, 1, mockClient.sendChatCalls)
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

func TestLazyClient_ConcurrentInit_ErrorCached(t *testing.T) {
	var initCount atomic.Int32
	simulatedErr := errors.New("init failed")

	lc := newLazyClient(func() (llm.ExtendedClient, error) {
		initCount.Add(1)
		return nil, simulatedErr
	})

	const numGoroutines = 10
	var wg sync.WaitGroup
	errs := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _, errs[idx] = lc.Generate(context.Background(), nil, nil, nil)
		}(i)
	}
	wg.Wait()

	// Factory must be called exactly once
	assert.Equal(t, int32(1), initCount.Load())

	for i := 0; i < numGoroutines; i++ {
		require.Error(t, errs[i], "goroutine %d should have received an error", i)
		assert.Contains(t, errs[i].Error(), "LLM provider initialization failed",
			"goroutine %d error should contain wrapping prefix", i)
		assert.ErrorIs(t, errs[i], simulatedErr,
			"goroutine %d error should wrap the original init error", i)
	}
}
