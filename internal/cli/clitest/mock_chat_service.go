// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package clitest

import (
	"context"
	"io"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Compile-time guard: MockChatService must implement agent.ChatService.
var _ agent.ChatService = (*MockChatService)(nil)

// MockChatService is a hand-rolled test double for agent.ChatService.
// Override function fields to script behaviour. All methods record
// invocation counts and names accessible via Snapshot().
type MockChatService struct {
	mu                       sync.Mutex
	calledProcessMessage     int
	calledGetLastUserMessage int
	calledBrowseHistory      int
	calledGetToolNames       int
	calledStreamTurnsLog     int
	calledRunDiagnostics     int
	calledMethods            []string

	// Function fields — set before test to script behaviour.
	ProcessMessageFunc     func(ctx context.Context, cfg *domain_config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error
	GetLastUserMessageFunc func(ctx context.Context, hManager ports.HistoryManager) (string, int, error)
	BrowseHistoryFunc      func(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error
	GetToolNamesFunc       func(ctx context.Context, reg tools.Registry) ([]string, error)
	StreamTurnsLogFunc     func(ctx context.Context, cfg *domain_config.Config, out io.Writer) error
	RunDiagnosticsFunc     func(ctx context.Context, cfg *domain_config.Config, configPath string, jsonOutput bool) error

	// Legacy convenience fields for migration compatibility.
	// These are set by ProcessMessage when the function field is overridden
	// by the test with a closure that writes to them. Tests that rely on
	// the spy counters and Snapshot() do not need these fields.
	ChatCalled bool
	LastParams agent.ChatCommand
}

// ChatServiceSnapshot holds a race-safe copy of mock call counts and
// ordered method names.
type ChatServiceSnapshot struct {
	ProcessMessage, GetLastUserMessage, BrowseHistory int
	GetToolNames, StreamTurnsLog, RunDiagnostics      int
	Methods                                           []string
}

// Snapshot returns a race-safe copy of invocation counts and ordered
// method names. The returned Methods slice is a defensive copy safe
// for inspection without holding the mutex.
func (m *MockChatService) Snapshot() ChatServiceSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	methods := make([]string, len(m.calledMethods))
	copy(methods, m.calledMethods)
	return ChatServiceSnapshot{
		ProcessMessage:     m.calledProcessMessage,
		GetLastUserMessage: m.calledGetLastUserMessage,
		BrowseHistory:      m.calledBrowseHistory,
		GetToolNames:       m.calledGetToolNames,
		StreamTurnsLog:     m.calledStreamTurnsLog,
		RunDiagnostics:     m.calledRunDiagnostics,
		Methods:            methods,
	}
}

// ProcessMessage handles the entire business flow of a chat turn.
// When ProcessMessageFunc is nil the default is success (nil error).
func (m *MockChatService) ProcessMessage(ctx context.Context, cfg *domain_config.Config, cmd agent.ChatCommand, capturer agent.CapturerInteractor) error {
	m.mu.Lock()
	m.calledProcessMessage++
	m.calledMethods = append(m.calledMethods, "ProcessMessage")
	m.mu.Unlock()

	if m.ProcessMessageFunc != nil {
		return m.ProcessMessageFunc(ctx, cfg, cmd, capturer)
	}
	return nil
}

// GetLastUserMessage retrieves the last user message and the number of
// turns to rollback to reach that point. When GetLastUserMessageFunc is
// nil the default returns ("", 0, nil).
func (m *MockChatService) GetLastUserMessage(ctx context.Context, hManager ports.HistoryManager) (string, int, error) {
	m.mu.Lock()
	m.calledGetLastUserMessage++
	m.calledMethods = append(m.calledMethods, "GetLastUserMessage")
	m.mu.Unlock()

	if m.GetLastUserMessageFunc != nil {
		return m.GetLastUserMessageFunc(ctx, hManager)
	}
	return "", 0, nil
}

// BrowseHistory starts the TUI browser for chat history.
// When BrowseHistoryFunc is nil the default is success (nil error).
func (m *MockChatService) BrowseHistory(ctx context.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
	m.mu.Lock()
	m.calledBrowseHistory++
	m.calledMethods = append(m.calledMethods, "BrowseHistory")
	m.mu.Unlock()

	if m.BrowseHistoryFunc != nil {
		return m.BrowseHistoryFunc(ctx, provider, hManager)
	}
	return nil
}

// GetToolNames retrieves the names of all available tools.
// When GetToolNamesFunc is nil the default returns ([]string{"test_tool"}, nil),
// preserving the existing mock behaviour from chat_command_test.go.
func (m *MockChatService) GetToolNames(ctx context.Context, reg tools.Registry) ([]string, error) {
	m.mu.Lock()
	m.calledGetToolNames++
	m.calledMethods = append(m.calledMethods, "GetToolNames")
	m.mu.Unlock()

	if m.GetToolNamesFunc != nil {
		return m.GetToolNamesFunc(ctx, reg)
	}
	return []string{"test_tool"}, nil
}

// StreamTurnsLog resolves the turns log path for the current mode and
// streams it to the provided writer. When StreamTurnsLogFunc is nil
// the default is success (nil error).
func (m *MockChatService) StreamTurnsLog(ctx context.Context, cfg *domain_config.Config, out io.Writer) error {
	m.mu.Lock()
	m.calledStreamTurnsLog++
	m.calledMethods = append(m.calledMethods, "StreamTurnsLog")
	m.mu.Unlock()

	if m.StreamTurnsLogFunc != nil {
		return m.StreamTurnsLogFunc(ctx, cfg, out)
	}
	return nil
}

// RunDiagnostics performs a comprehensive system health check.
// When RunDiagnosticsFunc is nil the default is success (nil error).
func (m *MockChatService) RunDiagnostics(ctx context.Context, cfg *domain_config.Config, configPath string, jsonOutput bool) error {
	m.mu.Lock()
	m.calledRunDiagnostics++
	m.calledMethods = append(m.calledMethods, "RunDiagnostics")
	m.mu.Unlock()

	if m.RunDiagnosticsFunc != nil {
		return m.RunDiagnosticsFunc(ctx, cfg, configPath, jsonOutput)
	}
	return nil
}
