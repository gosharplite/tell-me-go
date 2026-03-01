// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestContextPrepAdapter_Prepare(t *testing.T) {
	history := &mockHistoryManager{
		contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		},
	}
	strategy := NewContextStrategy(&mockTokenCounter{}, nil)
	cm := NewContextManager(strategy, history, nil, nil)

	adapter := NewContextPrepAdapter(cm)

	t.Run("successful prepare", func(t *testing.T) {
		ctx := context.Background()
		prepared, _, err := adapter.Prepare(ctx, 1)
		assert.NoError(t, err)
		assert.Len(t, prepared, 1)
		assert.Equal(t, "hello", prepared[0].Parts[0].Text)
	})

	t.Run("prepare error", func(t *testing.T) {
		history.getWindowErr = errors.New("get window failed")
		ctx := context.Background()
		_, _, err := adapter.Prepare(ctx, 1)
		assert.Error(t, err)
		assert.Equal(t, "get window failed", err.Error())
		history.getWindowErr = nil
	})
}

func TestContextPrepAdapter_AddContent(t *testing.T) {
	history := &mockHistoryManager{}
	strategy := NewContextStrategy(&mockTokenCounter{}, nil)
	cm := NewContextManager(strategy, history, nil, nil)

	adapter := NewContextPrepAdapter(cm)

	t.Run("successful add content", func(t *testing.T) {
		ctx := context.Background()
		content := &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "new content"}}}
		err := adapter.AddContent(ctx, content)
		assert.NoError(t, err)
		assert.Len(t, history.contents, 1)
		assert.Equal(t, "new content", history.contents[0].Parts[0].Text)
	})

	t.Run("add content error (context cancelled)", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		content := &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "cancelled"}}}
		err := adapter.AddContent(ctx, content)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
	})
}
