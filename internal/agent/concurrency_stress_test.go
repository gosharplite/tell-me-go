// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestAgent_Concurrency_ConfigRace(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	hManager := history.NewManager(tmpDir + "/history.json")
	reg := registry.New()
	sm := security.NewSecurityManager(nil)

	// Register a slow tool to keep the agent busy
	reg.Register(&tools.ToolDeclaration{
		Name: "slow_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		time.Sleep(50 * time.Millisecond)
		return tools.ToolResult{Text: "done"}, nil
	})

	mockClient := &mockLLMClient{
		sendChatFn: func(ctx context.Context, history []*llm.Content, t []*tools.ToolDeclaration) (*llm.Content, *llm.Metrics, error) {
			// Logic for slow tool
			if len(history) < 4 {
				return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "slow_tool"}}}}, &llm.Metrics{}, nil
			}
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "final"}}}, &llm.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, reg, sm, true)
	session := &Session{History: hManager, StartTime: time.Now()}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: Run Chat
	go func() {
		defer wg.Done()
		_ = a.Chat(ctx, session, "start")
	}()

	// Goroutine 2: Hammer configuration updates
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			a.SetLimits(10, 1000+i, 20)
			a.SetConcurrency(5+(i%5), 30)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()
}

type mockLLMClient struct {
	sendChatFn func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration) (*llm.Content, *llm.Metrics, error)
}

func (m *mockLLMClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return m.sendChatFn(ctx, history, tools)
}

func (m *mockLLMClient) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	content, metrics, err := m.sendChatFn(ctx, history, tools)
	if err == nil {
		callback(content)
	}
	return metrics, err
}

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *mockLLMClient) RefreshAuth() error {
	return nil
}

func TestToolExecutor_ConcurrentExecutionAndConfig(t *testing.T) {
	reg := registry.New()
	bus := &events.TestEventBus{}
	sm := security.NewSecurityManager(nil)
	exec := executor.NewToolExecutor(reg, sm, bus)

	reg.Register(&tools.ToolDeclaration{Name: "task"}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		time.Sleep(10 * time.Millisecond)
		return tools.ToolResult{Text: "ok"}, nil
	})

	ctx := context.Background()
	var wg sync.WaitGroup

	// Run concurrent executions
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			content := &llm.Content{
				Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "task"}}},
			}
			_, _ = exec.Execute(ctx, content, 0, 10)
		}(i)
	}

	// Hammer config updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			exec.SetConcurrency(1+(i%10), time.Duration(10+i)*time.Second)
		}
	}()

	wg.Wait()

	// Verify observability via TestEventBus
	importReflect := reflect.TypeOf(events.ToolCallEvent{})
	if !bus.AssertEventPublished(importReflect) {
		t.Error("Expected ToolCallEvent to be published")
	}
}
