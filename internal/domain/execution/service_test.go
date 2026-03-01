// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockRegistry is a mock for tools.IToolRegistry.
type mockRegistry struct {
	mock.Mock
}

var _ tools.IToolRegistry = (*mockRegistry)(nil)

func (m *mockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) {
	m.Called(def, handler)
}

func (m *mockRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) {
	m.Called(def, handler, opts)
}

func (m *mockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	callArgs := m.Called(ctx, name, args)
	return callArgs.Get(0).(tools.ToolResult), callArgs.Error(1)
}

func (m *mockRegistry) IsSerial(name string) bool {
	return m.Called(name).Bool(0)
}

func (m *mockRegistry) IsLongRunning(name string) bool {
	return m.Called(name).Bool(0)
}

func (m *mockRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return m.Called().Get(0).([]*tools.ToolDeclaration)
}

// mockSecurityManager is a mock for security.ISecurityManager.
type mockSecurityManager struct {
	mock.Mock
}

var _ security.ISecurityManager = (*mockSecurityManager)(nil)

func (m *mockSecurityManager) IsPathSafe(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}

func (m *mockSecurityManager) IsPathWritable(path string) (string, error) {
	args := m.Called(path)
	return args.String(0), args.Error(1)
}

func (m *mockSecurityManager) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	args := m.Called(ctx, action, target, detail)
	return args.Bool(0), args.Error(1)
}

func (m *mockSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	args := m.Called(ctx, label, detail, reason, isSafe)
	return args.Bool(0), args.Error(1)
}

func (m *mockSecurityManager) LogAudit(label1, val1, label2, val2 string) {
	m.Called(label1, val1, label2, val2)
}

func (m *mockSecurityManager) TerminalLock()   { m.Called() }
func (m *mockSecurityManager) TerminalUnlock() { m.Called() }
func (m *mockSecurityManager) Prompt(message string) { m.Called(message) }
func (m *mockSecurityManager) Warn(message string)   { m.Called(message) }
func (m *mockSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}
func (m *mockSecurityManager) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *mockSecurityManager) IsCommandAllowed(command string) bool {
	return m.Called(command).Bool(0)
}
func (m *mockSecurityManager) IsBypassActive() bool {
	return m.Called().Bool(0)
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name          string
		content       *llm.Content
		turn          int
		maxTurns      int
		setupMock     func(reg *mockRegistry, sm *mockSecurityManager)
		expectedError string
		expectedCount int
	}{
		{
			name: "Happy path - tool executes successfully",
			content: &llm.Content{
				Parts: []*llm.Part{
					{FunctionCall: &llm.FunctionCall{ID: "1", Name: "ls", Args: map[string]interface{}{"path": "."}}},
				},
			},
			turn:     1,
			maxTurns: 5,
			setupMock: func(reg *mockRegistry, sm *mockSecurityManager) {
				reg.On("Execute", mock.Anything, "ls", mock.Anything).Return(tools.ToolResult{Text: "file1.go"}, nil)
			},
			expectedCount: 1,
		},
		{
			name: "Security block - SecurityManager rejects tool",
			content: &llm.Content{
				Parts: []*llm.Part{
					{FunctionCall: &llm.FunctionCall{ID: "1", Name: "rm", Args: map[string]interface{}{"command": "rm -rf /"}}},
				},
			},
			turn:     1,
			maxTurns: 5,
			setupMock: func(reg *mockRegistry, sm *mockSecurityManager) {
				sm.On("IsCommandAllowed", "rm -rf /").Return(false)
			},
			expectedCount: 1,
		},
		{
			name: "Execution failure - Tool returns error",
			content: &llm.Content{
				Parts: []*llm.Part{
					{FunctionCall: &llm.FunctionCall{ID: "1", Name: "ls", Args: map[string]interface{}{"path": "/invalid"}}},
				},
			},
			turn:     1,
			maxTurns: 5,
			setupMock: func(reg *mockRegistry, sm *mockSecurityManager) {
				reg.On("Execute", mock.Anything, "ls", mock.Anything).Return(tools.ToolResult{}, errors.New("execution error"))
			},
			expectedCount: 1,
		},
		{
			name: "Unknown tool call",
			content: &llm.Content{
				Parts: []*llm.Part{
					{FunctionCall: &llm.FunctionCall{ID: "1", Name: "unknown", Args: map[string]interface{}{}}},
				},
			},
			turn:     1,
			maxTurns: 5,
			setupMock: func(reg *mockRegistry, sm *mockSecurityManager) {
				reg.On("Execute", mock.Anything, "unknown", mock.Anything).Return(tools.ToolResult{}, errors.New("unknown tool"))
			},
			expectedCount: 1,
		},
		{
			name: "Max turns reached",
			content: &llm.Content{
				Parts: []*llm.Part{
					{FunctionCall: &llm.FunctionCall{ID: "1", Name: "ls", Args: map[string]interface{}{"path": "."}}},
				},
			},
			turn:          5,
			maxTurns:      5,
			setupMock:     func(reg *mockRegistry, sm *mockSecurityManager) {},
			expectedError: "maximum tool execution turns reached",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := &mockRegistry{}
			sm := &mockSecurityManager{}
			tt.setupMock(reg, sm)
			service := NewService(WithRegistry(reg), WithSecurity(sm))

			res, err := service.Execute(context.Background(), tt.content, tt.turn, tt.maxTurns)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				if tt.expectedCount > 0 {
					assert.NotNil(t, res)
					assert.Equal(t, tt.expectedCount, len(res.Parts))
				}
			}
			reg.AssertExpectations(t)
			sm.AssertExpectations(t)
		})
	}
}
