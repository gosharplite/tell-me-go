// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
)

func TestAgent_Concurrency_ConfigRace(t *testing.T) {
	// Setup
	tmpDir := t.TempDir()
	hManager := history.NewManager(infrapersistence.NewOSFileSystem(), tmpDir+"/history.json", tmpDir+"/history.archive.jsonl")
	reg := registry.New()
	sm := security.NewSecurityManager(nil)

	// Register a slow tool to keep the agent busy
	reg.Register(&tools.ToolDeclaration{
		Name: "slow_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		time.Sleep(50 * time.Millisecond)
		return tools.ToolResult{Text: "done"}, nil
	})

	mockClient := &stressmockLLMClient{
		sendChatFn: func(ctx context.Context, history []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			// Logic for slow tool
			if len(history) < 4 {
				return &llm.Content{Role: "model", Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "slow_tool"}}}}, &llm.Metrics{}, nil
			}
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "final"}}}, &llm.Metrics{}, nil
		},
	}

	bus := events.NewSimpleEventBus()
	a := New(mockClient, bus, hManager, "test-provider", reg, sm)
	session := &services.Session{History: hManager, StartTime: time.Now()}

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
			_ = a.SetLimits(ctx, 10, 1000+i, 20)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()
}

type stressmockLLMClient struct {
	sendChatFn func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
}

func (m *stressmockLLMClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return m.sendChatFn(ctx, history, tools, resolver)
}

func (m *stressmockLLMClient) StreamChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
	content, metrics, err := m.sendChatFn(ctx, history, tools, resolver)
	if err == nil {
		callback(content)
	}
	return metrics, err
}

func (m *stressmockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *stressmockLLMClient) RefreshAuth() error {
	return nil
}

func (m *stressmockLLMClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	outCh := make(chan *llm.Content, 1)
	resCh := make(chan struct {
		content *llm.Content
		metrics *llm.Metrics
		err     error
	}, 1)

	go func() {
		defer close(outCh)
		content, metrics, err := m.SendChat(ctx, input, tools, resolver)
		if err == nil {
			outCh <- content
		}
		resCh <- struct {
			content *llm.Content
			metrics *llm.Metrics
			err     error
		}{content, metrics, err}
	}()

	finalize := func() (*llm.Content, *llm.Metrics, error) {
		res := <-resCh
		return res.content, res.metrics, res.err
	}

	return outCh, finalize
}

func TestToolExecutor_ConcurrentExecutionAndConfig(t *testing.T) {
	reg := registry.New()
	bus := &inframock.TestEventBus{}
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

func TestContextManager_Race(t *testing.T) {
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), tmpDir+"/history.json", tmpDir+"/history.archive.jsonl")
	bus := &events.SimpleEventBus{}
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(nil), bus)
	factory := &orchestration.PipelineFactory{
		Estimator: strategy,
		Events:    bus,
		History:   h,
	}
	cm := orchestration.NewContextManager(strategy, h, bus, factory)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(4)

	// Goroutine 1: Prepare (Simulate turn start)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_, _, _ = cm.Prepare(ctx, i)
			time.Sleep(2 * time.Millisecond)
		}
	}()

	// Goroutine 2: AddContent (Simulate adding model response)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			_ = cm.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "ping"}}})
			_ = cm.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "pong"}}})
			time.Sleep(3 * time.Millisecond)
		}
	}()

	// Goroutine 3: Config updates (Simulate dynamic limits)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			bus.Publish(events.ConfigUpdated{
				Limits: events.Limits{
					MaxHistoryTokens: 1000 + i,
					MaxToolTurns:     10,
					MaxHistoryTurns:  20,
				},
			})
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Goroutine 4: Summarize (Simulate background/ad-hoc summarization)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			_, _, _ = cm.SummarizeRange(ctx, 2, "focus")
			time.Sleep(10 * time.Millisecond)
		}
	}()

	wg.Wait()
}

func TestTurnEngine_Concurrency_TaskCost(t *testing.T) {
	// Setup
	reg := registry.New()
	bus := &events.SimpleEventBus{}

	// Create a single engine instance
	gw := &mockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
			ch := make(chan *llm.Content)
			close(ch)
			return ch, func() (*llm.Content, *llm.Metrics, error) {
				return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}, &llm.Metrics{
					PromptTokens:   1000,
					ResponseTokens: 1000,
				}, nil
			}
		},
	}

	executor := &mockExecutor{
		ExecuteFunc: func(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error) {
			return nil, nil
		},
	}

	h := history.NewManager(infrapersistence.NewOSFileSystem(), "", "")
	strategy := orchestration.NewContextStrategy(orchestration.NewHeuristicTokenCounter(reg), bus)
	cm := orchestration.NewContextManager(strategy, h, bus, nil)
	cm.Pipeline = orchestration.NewContextPipeline()

	tracker := &mockEngineCostTracker{} // Returns 0.05 per call

	e := newTurnEngine(gw, executor, cm, reg, bus, withCostTracker(tracker))
	strategy.SetLimits(10000, 10, 10)

	var wg sync.WaitGroup
	numConcurrent := 20
	wg.Add(numConcurrent)

	// We'll use a local bus for each goroutine if we wanted to verify perfectly,
	// but here we just want to ensure NO PANICS and NO RACE conditions under -race.
	for i := 0; i < numConcurrent; i++ {
		go func() {
			defer wg.Done()
			_ = e.Run(context.Background(), time.Now())
		}()
	}

	wg.Wait()
}
