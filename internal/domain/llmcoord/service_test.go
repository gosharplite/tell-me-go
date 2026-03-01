// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llmcoord

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockGateway is a mock for llm.LLMGateway.
type mockGateway struct {
	mock.Mock
}

var _ llm.LLMGateway = (*mockGateway)(nil)

func (m *mockGateway) Generate(ctx context.Context, input []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	args := m.Called(ctx, input, toolDecls, resolver)
	return args.Get(0).(<-chan *llm.Content), args.Get(1).(func() (*llm.Content, *llm.Metrics, error))
}

func TestGenerate(t *testing.T) {
	tests := []struct {
		name          string
		setupMock     func(m *mockGateway)
		expectedError string
	}{
		{
			name: "Happy path - Successful LLM response",
			setupMock: func(m *mockGateway) {
				respCh := make(chan *llm.Content)
				close(respCh)
				finalize := func() (*llm.Content, *llm.Metrics, error) {
					return &llm.Content{Role: "assistant"}, &llm.Metrics{}, nil
				}
				m.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return((<-chan *llm.Content)(respCh), finalize)
			},
			expectedError: "",
		},
		{
			name: "Gateway timeout / error",
			setupMock: func(m *mockGateway) {
				respCh := make(chan *llm.Content)
				close(respCh)
				finalize := func() (*llm.Content, *llm.Metrics, error) {
					return nil, nil, errors.New("gateway error")
				}
				m.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return((<-chan *llm.Content)(respCh), finalize)
			},
			expectedError: "gateway error",
		},
		{
			name: "API returned nil content",
			setupMock: func(m *mockGateway) {
				respCh := make(chan *llm.Content)
				close(respCh)
				finalize := func() (*llm.Content, *llm.Metrics, error) {
					return nil, nil, nil
				}
				m.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return((<-chan *llm.Content)(respCh), finalize)
			},
			expectedError: "api returned nil content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &mockGateway{}
			tt.setupMock(m)
			service := NewService(WithGateway(m))

			content, metrics, err := service.Generate(context.Background(), nil, nil, nil)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, content)
				assert.NotNil(t, metrics)
			}
			m.AssertExpectations(t)
		})
	}
}

func TestWithStreamHandler(t *testing.T) {
	m := &mockGateway{}
	respCh := make(chan *llm.Content, 1)
	respCh <- &llm.Content{Role: "assistant", Parts: []*llm.Part{{Text: "streaming"}}}
	close(respCh)
	finalize := func() (*llm.Content, *llm.Metrics, error) {
		return &llm.Content{Role: "assistant"}, &llm.Metrics{}, nil
	}
	m.On("Generate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return((<-chan *llm.Content)(respCh), finalize)

	handlerCalled := false
	handler := func(ctx context.Context, ch <-chan *llm.Content) {
		handlerCalled = true
		for range ch {
		}
	}

	service := NewService(WithGateway(m), WithStreamHandler(handler))
	_, _, err := service.Generate(context.Background(), nil, nil, nil)

	assert.NoError(t, err)
	assert.True(t, handlerCalled)
	m.AssertExpectations(t)
}
