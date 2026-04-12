// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor_test

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"github.com/stretchr/testify/require"
)

func SetupTestRegistry(t *testing.T, toolsMap map[string]executor.ToolBehavior) (tools.Registry, map[string]*executor.ToolBehavior) {
	t.Helper()
	reg := registry.New()
	behaviors := make(map[string]*executor.ToolBehavior)
	for name, behavior := range toolsMap {
		b := behavior
		behaviors[name] = &b
		opts := registry.ToolOptions{
			Serial:      b.Serial,
			LongRunning: b.Long,
		}
		if err := reg.RegisterWithOptions(&tools.ToolDeclaration{Name: name}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			if b.Observe != nil {
				b.Observe()
			}
			if b.Panic != nil {
				panic(b.Panic)
			}
			if b.Delay > 0 {
				timer := time.NewTimer(b.Delay)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return tools.ToolResult{}, ctx.Err()
				case <-timer.C:
				}
			}
			return b.Result, b.Err
		}, opts); err != nil {
			t.Fatalf("failed to register tool %s: %v", name, err)
		}
	}
	return reg, behaviors
}

func SetupTestExecutor(t *testing.T, toolsMap map[string]executor.ToolBehavior, allowedTools []string, opts ...executor.ExecutorOption) (*executor.Dispatcher, *inframock.TestEventBus, map[string]*executor.ToolBehavior) {
	reg, behaviors := SetupTestRegistry(t, toolsMap)
	sm := executor.SetupMockSecurityManager(allowedTools)

	bus := &inframock.TestEventBus{}
	exec, err := executor.NewPipelineDispatcher(reg, sm, bus, &ports.NoOpLogger{}, &executor.MockLogger{CriticalLogs: make(chan string, 10)}, opts...)
	require.NoError(t, err)

	return exec, bus, behaviors
}
