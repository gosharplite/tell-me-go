// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockLLMClient struct {
	mock.Mock
}

func (m *mockLLMClient) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	args := m.Called(ctx, history, toolDecls, resolver)
	return args.Get(0).(*llm.Content), args.Get(1).(*llm.Metrics), args.Error(2)
}

func (m *mockLLMClient) StreamChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	args := m.Called(ctx, history, toolDecls, resolver, callback)
	return args.Get(0).(*llm.Metrics), args.Error(1)
}

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	args := m.Called(ctx, model, prompt, mimeType)
	return args.Get(0).([][]byte), args.Error(1)
}

func (m *mockLLMClient) RefreshAuth() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockLLMClient) Generate(ctx context.Context, input []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	return nil, nil
}

type mockOnlyClient struct {
	mock.Mock
}

func (m *mockOnlyClient) SendChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return nil, nil, nil
}

func (m *mockOnlyClient) StreamChat(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	return nil, nil
}

func (m *mockOnlyClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *mockOnlyClient) RefreshAuth() error {
	return nil
}

func TestBuildSessionDependencies(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sm := internal_security.NewSecurityManager(nil)
	client := new(mockLLMClient)

	bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus) (llm.LLMClient, error) {
		return client, nil
	})

	cfg := &config.Config{
		Mode: "assistant",
		Models: map[string]config.ModelConfig{
			"test-model": {
				Pricing: pricing.ModelPricing{Comp: 0.01},
			},
		},
		Model: "test-model",
	}

	deps, hManager, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
	assert.NoError(t, err)
	assert.NotNil(t, deps)
	assert.NotNil(t, hManager)
	assert.NotNil(t, cleanup)

	cleanup()
}

func TestGetAgentFactory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "di-test-factory")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sm := internal_security.NewSecurityManager(nil)
	bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil)

	factory := bootstrapper.GetAgentFactory()
	assert.NotNil(t, factory)
}

func TestBootstrapper_Initialize_Errors(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-errors")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	tests := []struct {
		name          string
		setup         func() string // Returns homeDir for this test case
		clientFactory func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus) (llm.LLMClient, error)
		wantErr       string
	}{
		{
			name: "FailsOnInitializePathsError",
			setup: func() string {
				invalidHome := filepath.Join(tempDir, "a-file")
				os.WriteFile(invalidHome, []byte("test"), 0644)
				return invalidHome
			},
			wantErr: "failed to create session directory",
		},
		{
			name: "FailsOnHistoryLoadError",
			setup: func() string {
				home := filepath.Join(tempDir, "history-err-home")
				modeDir := filepath.Join(home, "output", cfg.Mode)
				os.MkdirAll(modeDir, 0755)
				historyPath := filepath.Join(modeDir, "history.jsonl")
				os.MkdirAll(historyPath, 0755) // Directory instead of file
				return home
			},
			wantErr: "error loading history",
		},
		{
			name: "FailsOnBadClientFactory",
			setup: func() string {
				return filepath.Join(tempDir, "factory-err-home")
			},
			clientFactory: func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus) (llm.LLMClient, error) {
				return nil, errors.New("simulated factory failure")
			},
			wantErr: "simulated factory failure",
		},
		{
			name: "FailsOnClientNotLLMGateway",
			setup: func() string {
				return filepath.Join(tempDir, "gateway-err-home")
			},
			clientFactory: func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus) (llm.LLMClient, error) {
				return &mockOnlyClient{}, nil
			},
			wantErr: "client does not implement LLMGateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			homeDir := tt.setup()
			b := NewBootstrapper(homeDir, internal_security.NewSecurityManager(nil), "1.0.0", io.Discard, io.Discard, tt.clientFactory)
			_, _, _, err := b.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestFinalizeSession(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-finalize")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sm := internal_security.NewSecurityManager(nil)
	cfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
	}

	client := new(mockLLMClient)
	b := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus) (llm.LLMClient, error) {
		return client, nil
	})

	deps, hManager, cleanup, err := b.BuildSessionDependencies(ctx, cfg, "config.yaml", false, nil)
	assert.NoError(t, err)
	defer cleanup()

	b.FinalizeSession(ctx, hManager, deps, cfg)
}

func TestGetAgentFactory_Execution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "di-test-factory-exec")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sm := internal_security.NewSecurityManager(nil)
	bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, nil)

	factory := bootstrapper.GetAgentFactory()
	assert.NotNil(t, factory)

	// Execute the factory
	bus := events.NewSimpleEventBus()
	client := new(mockLLMClient)
	hManager := history.NewManager(nil, "history.jsonl", "archive.jsonl")
	reg := registry.New()

	agent := factory(nil, client, hManager, reg, sm, false, bus, "test-provider", "test-model", "assistant", "tokens.log", nil, nil)
	assert.NotNil(t, agent)
}

func TestBuildSessionDependencies_NewSession(t *testing.T) {
	ctx := context.Background()
	tempDir, err := os.MkdirTemp("", "di-test-new-session")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sm := internal_security.NewSecurityManager(nil)
	client := new(mockLLMClient)

	bootstrapper := NewBootstrapper(tempDir, sm, "1.0.0", io.Discard, io.Discard, func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus) (llm.LLMClient, error) {
		return client, nil
	})

	cfg := &config.Config{
		Mode: "assistant",
		Models: map[string]config.ModelConfig{
			"test-model": {
				Pricing: pricing.ModelPricing{Comp: 0.01},
			},
		},
		Model: "test-model",
	}

	deps, hManager, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, cfg, "config.yaml", true, nil)
	assert.NoError(t, err)
	assert.NotNil(t, deps)
	assert.NotNil(t, hManager)
	assert.NotNil(t, cleanup)

	cleanup()
}
