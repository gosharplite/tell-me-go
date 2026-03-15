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

func (m *integrationToolRegistry) Register(declaration *tools.ToolDeclaration, implementation tools.ToolFunc) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.declarations[declaration.Name] = declaration
	m.handlers[declaration.Name] = implementation
	return nil
}

func (m *integrationToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	m.mu.Lock()
	m.serial[def.Name] = opts.Serial
	m.longRunning[def.Name] = opts.LongRunning
	m.mu.Unlock()
	return m.Register(def, handler)
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

func (s *integrationSecurityManager) IsPathSafe(path string) (string, error)     { return path, nil }
func (s *integrationSecurityManager) IsPathWritable(path string) (string, error) { return path, nil }
func (s *integrationSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}
func (s *integrationSecurityManager) LogAudit(action string, args ...any) {}
func (s *integrationSecurityManager) TerminalLock()                       {}
func (s *integrationSecurityManager) TerminalUnlock()                     {}
func (s *integrationSecurityManager) Prompt(message string)               {}
func (s *integrationSecurityManager) Warn(message string)                 {}
func (s *integrationSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (s *integrationSecurityManager) ReadLine(ctx context.Context) (string, error) { return "", nil }
func (s *integrationSecurityManager) IsCommandAllowed(command string) bool         { return true }
func (s *integrationSecurityManager) IsBypassActive() bool                         { return false }
func (s *integrationSecurityManager) Close() error                                 { return nil }

func TestToolExecutor_EndToEnd_BarrierPattern(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}

	reg := newIntegrationToolRegistry()
	bus := &mockEventBus{} // from mocks_test.go
	sm := &integrationSecurityManager{}

	exec, err := executor.NewToolExecutor(reg, sm, bus, &executor.MockLogger{CriticalLogs: make(chan string, 10)}, executor.WithLongRunningTimeout(500*time.Millisecond))
	require.NoError(t, err)
	defer exec.Shutdown()

	var executionCounter int32
	var parallelOrder int32
	var serialOrder int32

	err = reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "mock_parallel",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		atomic.StoreInt32(&parallelOrder, atomic.AddInt32(&executionCounter, 1))
		return tools.ToolResult{Text: "parallel done"}, nil
	}, tools.ToolOptions{Serial: false, LongRunning: false})
	require.NoError(t, err)

	err = reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "mock_serial",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		atomic.StoreInt32(&serialOrder, atomic.AddInt32(&executionCounter, 1))
		return tools.ToolResult{Text: "serial done"}, nil
	}, tools.ToolOptions{Serial: true, LongRunning: true})
	require.NoError(t, err)

	// Simulate LLM response requesting both concurrently.
	// We put parallel FIRST to ensure it completes before the serial tool starts.
	resp := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "mock_parallel"}},
			{FunctionCall: &llm.FunctionCall{Name: "mock_serial"}},
		},
	}

	_, err = exec.Execute(context.Background(), resp, 0, 10)
	require.NoError(t, err)

	// Assertions
	pOrder := atomic.LoadInt32(&parallelOrder)
	sOrder := atomic.LoadInt32(&serialOrder)

	assert.NotZero(t, pOrder, "Parallel tool should have finished")
	assert.NotZero(t, sOrder, "Serial tool should have started")
	assert.True(t, pOrder < sOrder, "Sequential Integrity Failure: Parallel tool must finish BEFORE subsequent serial tool starts.")
}

func TestToolExecutor_EndToEnd_SequentialOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}

	reg := newIntegrationToolRegistry()
	bus := &mockEventBus{}
	sm := &integrationSecurityManager{}

	exec, err := executor.NewToolExecutor(reg, sm, bus, &executor.MockLogger{CriticalLogs: make(chan string, 10)}, executor.WithLongRunningTimeout(500*time.Millisecond))
	require.NoError(t, err)
	defer exec.Shutdown()

	var executionCounter int32
	var serialOrder int32
	var parallelOrder int32

	err = reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "mock_serial",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		atomic.StoreInt32(&serialOrder, atomic.AddInt32(&executionCounter, 1))
		return tools.ToolResult{Text: "serial done"}, nil
	}, tools.ToolOptions{Serial: true, LongRunning: true})
	require.NoError(t, err)

	err = reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "mock_parallel",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		atomic.StoreInt32(&parallelOrder, atomic.AddInt32(&executionCounter, 1))
		return tools.ToolResult{Text: "parallel done"}, nil
	}, tools.ToolOptions{Serial: false, LongRunning: false})
	require.NoError(t, err)

	// Put serial FIRST. In the new implementation, it MUST run first.
	resp := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "mock_serial"}},
			{FunctionCall: &llm.FunctionCall{Name: "mock_parallel"}},
		},
	}

	_, err = exec.Execute(context.Background(), resp, 0, 10)
	require.NoError(t, err)

	sOrder := atomic.LoadInt32(&serialOrder)
	pOrder := atomic.LoadInt32(&parallelOrder)

	assert.NotZero(t, sOrder)
	assert.NotZero(t, pOrder)
	assert.True(t, sOrder < pOrder, "Sequential Integrity Failure: Serial tool must finish BEFORE subsequent parallel tool starts.")
}

func TestToolExecutor_EndToEnd_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}

	timeout := 50 * time.Millisecond
	reg := newIntegrationToolRegistry()
	bus := &mockEventBus{}
	sm := &integrationSecurityManager{}

	exec, err := executor.NewToolExecutor(reg, sm, bus, &executor.MockLogger{CriticalLogs: make(chan string, 10)}, executor.WithLongRunningTimeout(timeout))
	require.NoError(t, err)
	defer exec.Shutdown()

	exitSignal := make(chan struct{})

	regErr := reg.RegisterWithOptions(&tools.ToolDeclaration{
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
	require.NoError(t, regErr)

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
