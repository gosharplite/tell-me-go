// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type mockExecutionObserver struct {
	mu            sync.Mutex
	timedOut      []string
	completedLate []string
}

func (m *mockExecutionObserver) ExecutionTimedOut(toolID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.timedOut = append(m.timedOut, toolID)
}

func (m *mockExecutionObserver) ExecutionCompletedLate(toolID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.completedLate = append(m.completedLate, toolID)
}

func (m *mockExecutionObserver) getTimedOut() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.timedOut
}

func (m *mockExecutionObserver) getCompletedLate() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.completedLate
}

func TestNewZombieTool(t *testing.T) {
	t.Run("nil observer returns error", func(t *testing.T) {
		z, err := NewZombieTool(nil)
		assert.Error(t, err)
		assert.Nil(t, z)
		assert.Equal(t, "ExecutionObserver is required", err.Error())
	})

	t.Run("valid observer returns ZombieTool", func(t *testing.T) {
		mock := &mockExecutionObserver{}
		z, err := NewZombieTool(mock)
		assert.NoError(t, err)
		assert.NotNil(t, z)
	})
}

func TestZombieTool_Monitor(t *testing.T) {
	t.Run("Late Completion", func(t *testing.T) {
		mock := &mockExecutionObserver{}
		z, _ := NewZombieTool(mock)
		outCh := make(chan ToolOutput, 1)

		ctx := context.Background()
		name := "test-tool"
		start := time.Now()
		timeout := 100 * time.Millisecond

		// Send value to outCh immediately
		go func() {
			outCh <- ToolOutput{}
		}()

		z.Monitor(ctx, name, start, outCh, timeout)

		assert.Len(t, mock.getCompletedLate(), 1)
		assert.Equal(t, name, mock.getCompletedLate()[0])
		assert.Len(t, mock.getTimedOut(), 0)
	})

	t.Run("Zombie Timeout", func(t *testing.T) {
		mock := &mockExecutionObserver{}
		z, _ := NewZombieTool(mock)
		outCh := make(chan ToolOutput, 1)

		ctx := context.Background()
		name := "zombie-tool"
		start := time.Now()
		timeout := 10 * time.Millisecond

		// Do NOT write to outCh
		z.Monitor(ctx, name, start, outCh, timeout)

		assert.Len(t, mock.getTimedOut(), 1)
		assert.Equal(t, name, mock.getTimedOut()[0])
		assert.Len(t, mock.getCompletedLate(), 0)
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		mock := &mockExecutionObserver{}
		z, _ := NewZombieTool(mock)
		outCh := make(chan ToolOutput, 1)

		ctx, cancel := context.WithCancel(context.Background())
		name := "cancelled-tool"
		start := time.Now()
		timeout := 100 * time.Millisecond

		// Cancel immediately
		cancel()

		z.Monitor(ctx, name, start, outCh, timeout)

		assert.Len(t, mock.getTimedOut(), 0)
		assert.Len(t, mock.getCompletedLate(), 0)
	})
}
