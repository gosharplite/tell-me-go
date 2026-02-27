// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_persistence "github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type mockChatter struct {
	chatCalled bool
	prompt     string
}

func (m *mockChatter) Chat(ctx stdctx.Context, s *ports.Session, prompt string) error {
	m.chatCalled = true
	m.prompt = prompt
	return nil
}

func (m *mockChatter) SetLimits(ctx stdctx.Context, toolTurns, historyTokens, historyTurns int) error {
	return nil
}

func (m *mockChatter) SetTieredThreshold(ctx stdctx.Context, threshold int) error { return nil }
func (m *mockChatter) Subscribe(sub func(events.Event))                           {}
func (m *mockChatter) Shutdown(ctx stdctx.Context) error                          { return nil }

type mockContainer struct {
	AgentFactory ports.ChatterFactory
	Client       domain_llm.LLMClient
}

func (m *mockContainer) BuildSessionDependencies(ctx stdctx.Context, cfg *domain_config.Config, configPath string, newSession bool, capturer domain_security.UserInteractor) (ports.SessionDependencies, *history.Manager, func(), error) {
	paths := &domain_persistence.Paths{
		LogPath: "/tmp/log",
	}
	hManager := history.NewManager(persistence.NewOSFileSystem(), "/tmp/h", "/tmp/ha")
	bus := events.NewSimpleEventBus()
	pricingData := domain_pricing.PricingData{}
	pricingOverrides := make(map[string]domain_pricing.ModelPricing)
	tracker := &mockTracker{}

	deps := orchestration.NewSessionDependencies(paths, hManager, m.Client, m.Client.(domain_llm.LLMGateway), nil, tracker, pricingData, pricingOverrides, bus)
	return deps, hManager, func() {}, nil
}

func (m *mockContainer) GetAgentFactory() ports.ChatterFactory {
	return m.AgentFactory
}

func (m *mockContainer) FinalizeSession(ctx stdctx.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *domain_config.Config) {
}

type mockTracker struct {
	domain_pricing.ICostTracker
}

func (m *mockTracker) Warmup() {}
func (m *mockTracker) GetStats(ctx stdctx.Context) (domain_pricing.UsageStats, float64) {
	return domain_pricing.UsageStats{}, 0
}

func TestChatCommand_Execute(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "assistant.yaml")
	if err := os.WriteFile(cfgPath, []byte("AIMODEL: test\nMODE: dev"), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	var stdout, stderr strings.Builder
	sm := internal_security.NewSecurityManager(nil)
	mChatter := &mockChatter{}
	mClient := &mockClient{}
	mContainer := &mockContainer{
		AgentFactory: func(params ports.ChatterParams) ports.Chatter {
			return mChatter
		},
		Client: mClient,
	}

	cmd := &chatCommand{
		Version:   "1.0.0",
		Stdin:     strings.NewReader("hello"),
		Stdout:    &stdout,
		Stderr:    &stderr,
		HomeDir:   tmpDir,
		Loader:    &mockLoader{},
		SM:        sm,
		Container: mContainer,
	}

	ctx := stdctx.Background()
	args := []string{"chat", "-c", cfgPath, "hello"}

	err := cmd.Execute(ctx, args)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mChatter.chatCalled {
		t.Error("expected chatter to be called")
	}
}

type mockClient struct {
	domain_llm.LLMClient
}

func (m *mockClient) Generate(ctx stdctx.Context, input []*domain_llm.Content, tools []*domaintools.ToolDeclaration, resolver domain_llm.AssetResolver) (<-chan *domain_llm.Content, func() (*domain_llm.Content, *domain_llm.Metrics, error)) {
	ch := make(chan *domain_llm.Content, 1)
	ch <- &domain_llm.Content{Parts: []*domain_llm.Part{{Text: "response"}}}
	close(ch)
	return ch, func() (*domain_llm.Content, *domain_llm.Metrics, error) {
		return &domain_llm.Content{Parts: []*domain_llm.Part{{Text: "response"}}}, &domain_llm.Metrics{}, nil
	}
}

func (m *mockClient) SendChat(ctx stdctx.Context, history []*domain_llm.Content, tools []*domaintools.ToolDeclaration, resolver domain_llm.AssetResolver) (*domain_llm.Content, *domain_llm.Metrics, error) {
	return nil, nil, nil
}
func (m *mockClient) StreamChat(ctx stdctx.Context, history []*domain_llm.Content, tools []*domaintools.ToolDeclaration, resolver domain_llm.AssetResolver, callback func(*domain_llm.Content)) (*domain_llm.Metrics, error) {
	return nil, nil
}
func (m *mockClient) GenerateImages(ctx stdctx.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}
func (m *mockClient) RefreshAuth() error {
	return nil
}

type mockLoader struct{}

func (m *mockLoader) Load(path string) (*domain_config.Config, error) {
	return &domain_config.Config{
		Model: "test",
		Mode:  "dev",
	}, nil
}
