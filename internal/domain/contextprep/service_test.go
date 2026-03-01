// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package contextprep

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockHistoryManager is a mock for ports.HistoryManager.
type mockHistoryManager struct {
	mock.Mock
}

var _ ports.HistoryManager = (*mockHistoryManager)(nil)

func (m *mockHistoryManager) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	args := m.Called(ctx, startIdx, endIdx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*llm.Content), args.Error(1)
}

func (m *mockHistoryManager) GetTotalEntries() int {
	args := m.Called()
	return args.Int(0)
}

func (m *mockHistoryManager) SetContents(ctx context.Context, contents []*llm.Content) error {
	args := m.Called(ctx, contents)
	return args.Error(0)
}

func (m *mockHistoryManager) Archive(ctx context.Context, contents []*llm.Content) error {
	args := m.Called(ctx, contents)
	return args.Error(0)
}

func (m *mockHistoryManager) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	args := m.Called(ctx, index, parts)
	return args.Error(0)
}

func (m *mockHistoryManager) AddContent(ctx context.Context, content *llm.Content) error {
	args := m.Called(ctx, content)
	return args.Error(0)
}

func (m *mockHistoryManager) GetResolver() llm.AssetResolver {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(llm.AssetResolver)
}

func (m *mockHistoryManager) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	args := m.Called(ctx, turnIndex, pinned)
	return args.Error(0)
}

func (m *mockHistoryManager) Save(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestPrepare(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func(m *mockHistoryManager)
		expectedError string
	}{
		{
			name: "Happy Path - Context successfully prepared",
			setupMock: func(m *mockHistoryManager) {
				m.On("GetWindow", mock.Anything, 0, -1).Return([]*llm.Content{
					{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
				}, nil)
			},
			expectedError: "",
		},
		{
			name: "Error state - History fails to load",
			setupMock: func(m *mockHistoryManager) {
				m.On("GetWindow", mock.Anything, 0, -1).Return(nil, errors.New("history error"))
			},
			expectedError: "failed to fetch history: history error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockHistoryManager{}
			tt.setupMock(m)
			service := NewService(WithHistory(m))

			contents, err := service.Prepare(context.Background(), 1)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, contents)
			}
			m.AssertExpectations(t)
		})
	}
}

func TestAddContent(t *testing.T) {
	tests := []struct {
		name          string
		content       *llm.Content
		setupMock     func(m *mockHistoryManager)
		expectedError string
	}{
		{
			name:    "First message is user",
			content: &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
			setupMock: func(m *mockHistoryManager) {
				m.On("GetTotalEntries").Return(0)
				m.On("AddContent", mock.Anything, mock.Anything).Return(nil)
			},
			expectedError: "",
		},
		{
			name:    "First message is assistant - Error",
			content: &llm.Content{Role: "assistant", Parts: []*llm.Part{{Text: "Hello"}}},
			setupMock: func(m *mockHistoryManager) {
				m.On("GetTotalEntries").Return(0)
			},
			expectedError: "first message must be 'user', got 'assistant'",
		},
		{
			name:    "Consecutive messages of same role - Appends parts",
			content: &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Part 2"}}},
			setupMock: func(m *mockHistoryManager) {
				m.On("GetTotalEntries").Return(1)
				m.On("GetWindow", mock.Anything, 0, -1).Return([]*llm.Content{
					{Role: "user", Parts: []*llm.Part{{Text: "Part 1"}}},
				}, nil)
				m.On("AppendParts", mock.Anything, 0, mock.Anything).Return(nil)
			},
			expectedError: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockHistoryManager{}
			tt.setupMock(m)
			service := NewService(WithHistory(m))

			err := service.AddContent(context.Background(), tt.content)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
			}
			m.AssertExpectations(t)
		})
	}
}
