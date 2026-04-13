// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor_test

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/require"
)

func SetupTestRegistry(t *testing.T, toolsMap map[string]testutil.ToolBehavior) (tools.Registry, map[string]*testutil.ToolBehavior) {
	t.Helper()
	reg := testutil.NewMockToolRegistry()
	behaviors := make(map[string]*testutil.ToolBehavior)
	for name, behavior := range toolsMap {
		b := behavior
		behaviors[name] = &b
		opts := tools.ToolOptions{
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

func SetupTestExecutor(t *testing.T, toolsMap map[string]testutil.ToolBehavior, allowedTools []string, opts ...executor.ExecutorOption) (*executor.Dispatcher, *testutil.MockEventBus, map[string]*testutil.ToolBehavior) {
	reg, behaviors := SetupTestRegistry(t, toolsMap)
	sm := testutil.SetupMockSecurityManager(allowedTools)

	bus := &testutil.MockEventBus{}
	exec, err := executor.NewPipelineDispatcher(reg, sm, bus, &ports.NoOpLogger{}, &testutil.MockLogger{CriticalLogs: make(chan string, 10)}, opts...)
	require.NoError(t, err)

	return exec, bus, behaviors
}
