// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// integrationToolRegistry is a registry that allows registering functions and querying serial/long-running status.
type integrationToolRegistry struct {
	mu           sync.RWMutex
	declarations map[string]*tools.ToolDeclaration
	handlers     map[string]tools.ToolFunc
	serial       map[string]bool
	longRunning  map[string]bool
}

func newIntegrationToolRegistry() *integrationToolRegistry {
	return &integrationToolRegistry{
		declarations: make(map[string]*tools.ToolDeclaration),
		handlers:     make(map[string]tools.ToolFunc),
		serial:       make(map[string]bool),
		longRunning:  make(map[string]bool),
	}
}

func (m *integrationToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	decls := make([]*tools.ToolDeclaration, 0, len(m.declarations))
	for _, d := range m.declarations {
		decls = append(decls, d)
	}
	return decls
}

func (m *integrationToolRegistry) Register(declaration *tools.ToolDeclaration, implementation tools.ToolFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.declarations[declaration.Name] = declaration
	m.handlers[declaration.Name] = implementation
}

func (m *integrationToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) {
	m.mu.Lock()
	m.serial[def.Name] = opts.Serial
	m.longRunning[def.Name] = opts.LongRunning
	m.mu.Unlock()
	m.Register(def, handler)
}

func (m *integrationToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	m.mu.RLock()
	handler, ok := m.handlers[name]
	m.mu.RUnlock()
	if !ok {
		return tools.ToolResult{}, errors.New("tool not found")
	}
	return handler(ctx, args)
}

func (m *integrationToolRegistry) IsSerial(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.serial[name]
}

func (m *integrationToolRegistry) IsLongRunning(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.longRunning[name]
}

// integrationSecurityManager implements security.ISecurityManager with all-allow defaults.
type integrationSecurityManager struct {
	security.ISecurityManager
}

func (s *integrationSecurityManager) IsPathSafe(path string) (string, error) { return path, nil }
func (s *integrationSecurityManager) IsPathWritable(path string) (string, error) { return path, nil }
func (s *integrationSecurityManager) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return true, nil
}
func (s *integrationSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}
func (s *integrationSecurityManager) LogAudit(label1, val1, label2, val2 string) {}
func (s *integrationSecurityManager) TerminalLock()                          {}
func (s *integrationSecurityManager) TerminalUnlock()                        {}
func (s *integrationSecurityManager) Prompt(message string)                  {}
func (s *integrationSecurityManager) Warn(message string)                    {}
func (s *integrationSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (s *integrationSecurityManager) ReadLine(ctx context.Context) (string, error) { return "", nil }
func (s *integrationSecurityManager) IsCommandAllowed(command string) bool         { return true }
func (s *integrationSecurityManager) IsBypassActive() bool                        { return false }

func TestToolExecutor_EndToEnd_BarrierPattern(t *testing.T) {
	// Setup
	reg := newIntegrationToolRegistry()
	bus := &mockEventBus{} // from mocks_test.go
	sm := &integrationSecurityManager{}

	exec := executor.NewToolExecutor(reg, sm, bus, executor.WithLongRunningTimeout(50*time.Millisecond))
	defer exec.Shutdown()

	var parallelFinishedAt int64
	var serialStartedAt int64

	reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "mock_parallel",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		time.Sleep(10 * time.Millisecond)
		atomic.StoreInt64(&parallelFinishedAt, time.Now().UnixNano())
		return tools.ToolResult{Text: "parallel done"}, nil
	}, tools.ToolOptions{Serial: false, LongRunning: false})

	reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "mock_serial",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		// Capture start time
		atomic.StoreInt64(&serialStartedAt, time.Now().UnixNano())
		return tools.ToolResult{Text: "serial done"}, nil
	}, tools.ToolOptions{Serial: true, LongRunning: true})

	// Simulate LLM response requesting both concurrently.
	// We put serial FIRST in the list to test that the barrier pattern reorders/prioritizes correctly.
	resp := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "mock_serial"}},
			{FunctionCall: &llm.FunctionCall{Name: "mock_parallel"}},
		},
	}

	_, err := exec.Execute(context.Background(), resp, 0, 10)
	require.NoError(t, err)

	// Assertions
	pEnd := atomic.LoadInt64(&parallelFinishedAt)
	sStart := atomic.LoadInt64(&serialStartedAt)

	assert.NotZero(t, pEnd, "Parallel tool should have finished")
	assert.NotZero(t, sStart, "Serial tool should have started")
	assert.True(t, pEnd < sStart, "Barrier Pattern Failure: Parallel tool must finish BEFORE serial tool starts. Parallel ended at %v, Serial started at %v", pEnd, sStart)
}

func TestToolExecutor_EndToEnd_ContextCancellation(t *testing.T) {
	// Setup with short timeout
	timeout := 50 * time.Millisecond
	reg := newIntegrationToolRegistry()
	bus := &mockEventBus{}
	sm := &integrationSecurityManager{}

	exec := executor.NewToolExecutor(reg, sm, bus, executor.WithLongRunningTimeout(timeout))
	defer exec.Shutdown()

	exitSignal := make(chan struct{})

	reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "mock_serial",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		select {
		case <-time.After(500 * time.Millisecond):
			return tools.ToolResult{Text: "finished late"}, nil
		case <-ctx.Done():
			close(exitSignal)
			return tools.ToolResult{}, ctx.Err()
		}
	}, tools.ToolOptions{Serial: true, LongRunning: true})

	resp := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "mock_serial"}},
		},
	}

	// 1. Verify internal timeout (from functional option)
	// We use a background context so the only timeout is the internal one (50ms)
	content, err := exec.Execute(context.Background(), resp, 0, 10)
	require.NoError(t, err, "Executor should not return error on internal tool timeout")
	require.NotEmpty(t, content.Parts)
	// The internal goroutine should still exit because runWithTimeout cancels its derived context
	select {
	case <-exitSignal:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Tool goroutine leaked after internal timeout")
	}

	// 2. Verify orchestration loop returns DeadlineExceeded when parent context expires
	// Reset exitSignal
	exitSignal = make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	
	_, err = exec.Execute(ctx, resp, 0, 10)
	assert.ErrorIs(t, err, context.DeadlineExceeded, "Orchestration loop should return DeadlineExceeded when parent context expires")

	// The internal goroutine should exit cleanly when the parent context is cancelled
	select {
	case <-exitSignal:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Tool goroutine leaked after parent context cancellation")
	}
}
