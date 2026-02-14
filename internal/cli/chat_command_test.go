// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	internal_security "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type mockChatter struct {
	chatCalled bool
	prompt     string
}

func (m *mockChatter) Chat(ctx stdctx.Context, s *services.Session, prompt string) error {
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

func TestChatCommand_Execute(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "vertex.yaml")
	if err := os.WriteFile(cfgPath, []byte("AIMODEL: test\nMODE: dev"), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	var stdout, stderr strings.Builder
	sm := internal_security.NewSecurityManager(nil)
	cmd := &chatCommand{
		Version: "1.0.0",
		Stdin:   strings.NewReader("hello"),
		Stdout:  &stdout,
		Stderr:  &stderr,
		HomeDir: tmpDir,
		Loader:  &mockLoader{},
		SM:      sm,
	}

	mChatter := &mockChatter{}
	cmd.AgentFactory = func(loader domain_config.ConfigLoader, client domain_llm.LLMGateway, hManager services.HistoryManager, reg domaintools.IToolRegistry, sm domain_security.ISecurityManager, disableStreaming bool, bus events.EventBus, model, mode, logPath string, pricingOverrides map[string]domain_pricing.ModelPricing, tracker domain_pricing.ICostTracker) services.Chatter {
		return mChatter
	}
	cmd.ClientFactory = func(cfg *domain_config.Config, p domain_pricing.PricingData, bus events.EventBus) (domain_llm.LLMClient, error) {
		return &mockClient{}, nil
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

type mockLoader struct{}

func (m *mockLoader) Load(path string) (*domain_config.Config, error) {
	return &domain_config.Config{
		Model: "test",
		Mode:  "dev",
	}, nil
}
