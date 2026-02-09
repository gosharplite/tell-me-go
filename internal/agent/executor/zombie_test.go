// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
)

func TestToolExecutor_ZombieToolMonitoring(t *testing.T) {
	reg := registry.New()

	// This tool ignores the context and sleeps for 200ms
	reg.Register(&tools.ToolDeclaration{Name: "zombie_tool"}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		time.Sleep(200 * time.Millisecond)
		return tools.ToolResult{Text: "I am a zombie"}, nil
	})

	bus := &events.SimpleEventBus{}
	var mu sync.Mutex
	var receivedEvents []events.SystemMessageEvent

	bus.Subscribe(func(e events.Event) {
		if se, ok := e.(events.SystemMessageEvent); ok {
			mu.Lock()
			receivedEvents = append(receivedEvents, se)
			mu.Unlock()
		}
	})

	exec := NewToolExecutor(reg, nil, bus)
	t.Cleanup(exec.Shutdown)
	exec.SetConcurrency(1, 50*time.Millisecond) // Short timeout

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "zombie_tool"}},
		},
	}

	_, _ = exec.Execute(context.Background(), content, 0, 5)

	// Wait for the zombie tool to finish and the watcher to publish the event
	// Timeout is 50ms, tool takes 200ms. So it should finish around 200ms mark.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	found := false
	for _, e := range receivedEvents {
		if strings.Contains(e.Message, "Telemetry: Non-compliant tool \"zombie_tool\" finally finished") {
			found = true
			if e.Level != "warn" {
				t.Errorf("Expected level 'warn', got %q", e.Level)
			}
			t.Logf("Received expected telemetry: %s", e.Message)
			break
		}
	}

	if !found {
		t.Error("Did not receive zombie tool telemetry event")
	}
}

func TestToolExecutor_ZombieToolPanicMonitoring(t *testing.T) {
	reg := registry.New()

	// This tool ignores the context, sleeps for 100ms, and then panics
	reg.Register(&tools.ToolDeclaration{Name: "panicking_zombie"}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		time.Sleep(100 * time.Millisecond)
		panic("boom")
	})

	bus := &events.SimpleEventBus{}
	var mu sync.Mutex
	var receivedEvents []events.SystemMessageEvent

	bus.Subscribe(func(e events.Event) {
		if se, ok := e.(events.SystemMessageEvent); ok {
			mu.Lock()
			receivedEvents = append(receivedEvents, se)
			mu.Unlock()
		}
	})

	exec := NewToolExecutor(reg, nil, bus)
	t.Cleanup(exec.Shutdown)
	exec.SetConcurrency(1, 20*time.Millisecond) // Short timeout

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "panicking_zombie"}},
		},
	}

	_, _ = exec.Execute(context.Background(), content, 0, 5)

	// Wait for the zombie tool to panic and the watcher to publish the event
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	foundPanic := false
	foundTelemetry := false
	for _, e := range receivedEvents {
		if strings.Contains(e.Message, "CRITICAL: Panic in tool executor while running \"panicking_zombie\": boom") {
			foundPanic = true
		}
		if strings.Contains(e.Message, "Telemetry: Non-compliant tool \"panicking_zombie\" finally finished") {
			foundTelemetry = true
		}
	}

	if !foundPanic {
		t.Error("Did not receive panic event for zombie tool")
	}
	if !foundTelemetry {
		t.Error("Did not receive zombie tool telemetry event for panicking tool")
	}
}
