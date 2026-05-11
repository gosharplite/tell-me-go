// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/di"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

// goldenPathMockClient is a minimal ExtendedClient that returns a simple
// text response with no tool calls — the golden path.
type goldenPathMockClient struct{}

func (m *goldenPathMockClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return &llm.Content{
		Role:  "model",
		Parts: []*llm.Part{{Text: "Hello from golden-path mock!"}},
	}, &llm.Metrics{PromptTokens: 5, ResponseTokens: 3}, nil
}

func (m *goldenPathMockClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return m.SendChat(ctx, input, tools, resolver)
}

func (m *goldenPathMockClient) GenerateImages(ctx context.Context, model, prompt, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *goldenPathMockClient) RefreshAuth() error { return nil }

// TestGoldenPath_ConfigToShutdown exercises the full vertical:
// DI bootstrap → agent init → chat → graceful shutdown.
// It runs in-process under -short and -race.
func TestGoldenPath_ConfigToShutdown(t *testing.T) {
	// Step 1 — Setup
	homeDir := t.TempDir()
	sm := security.NewSecurityManager(security.NoOpInteractor)
	defer func() { _ = sm.Close() }()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Step 2 — Create bootstrapper with mock LLM client
	bootstrapper := di.NewBootstrapper(homeDir, sm, "test-version", io.Discard, io.Discard, logger, nil,
		func(cfg *config.Config, pricingData pricing.PricingData, bus events.EventBus, logger ports.Logger) (llm.ExtendedClient, error) {
			return &goldenPathMockClient{}, nil
		},
	)

	// Step 3 — Build session dependencies
	cfg := &config.Config{
		Mode:  "assistant",
		Model: "test-model",
		Models: map[string]config.ModelConfig{
			"test-model": {Pricing: pricing.ModelPricing{Comp: 0.01}},
		},
	}

	ctx := context.Background()
	deps, hManager, cleanup, err := bootstrapper.BuildSessionDependencies(ctx, cfg, "", false, nil)
	if err != nil {
		t.Fatalf("BuildSessionDependencies failed: %v", err)
	}
	defer func() { _ = cleanup(ctx) }()

	// Step 4 — Create agent via factory
	factory := bootstrapper.GetAgentFactory()
	chatter, err := factory(ctx, deps, ports.ChatterConfig{
		ProviderName: "test-provider",
		Model:        "test-model",
		Mode:         "assistant",
	})
	if err != nil {
		t.Fatalf("AgentFactory failed: %v", err)
	}

	// Step 5 — Execute Chat (golden path)
	sess := ports.NewSession("golden-path", hManager)
	if err := chatter.Chat(ctx, sess, "Hello, golden path!"); err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Step 6 — Assert 2 messages in history (user + model)
	contents, histErr := hManager.GetWindow(ctx, 0, -1)
	if histErr != nil {
		t.Fatalf("GetWindow failed: %v", histErr)
	}
	if len(contents) != 2 {
		t.Fatalf("expected 2 messages in history (user + model), got %d", len(contents))
	}
	if len(contents[1].Parts) == 0 || contents[1].Parts[0].Text != "Hello from golden-path mock!" {
		t.Errorf("unexpected model response: %+v", contents[1])
	}

	// Step 7 — Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := chatter.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}
}
