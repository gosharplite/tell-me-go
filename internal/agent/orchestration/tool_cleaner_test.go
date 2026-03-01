// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/require"
)

func TestToolResponseCleaner_Transform(t *testing.T) {
	ctx := context.Background()
	cleaner := &toolResponseCleaner{}

	t.Run("Removes empty ID tool calls and responses", func(t *testing.T) {
		req := &ports.ContextRequest{
			History: []*llm.Content{
				{
					Role: "model",
					Parts: []*llm.Part{
						{FunctionCall: &llm.FunctionCall{ID: "valid", Name: "test"}},
						{FunctionCall: &llm.FunctionCall{ID: "", Name: "invalid"}},
					},
				},
				{
					Role: "user",
					Parts: []*llm.Part{
						{FunctionResponse: &llm.FunctionResponse{ID: "valid", Name: "test"}},
						{FunctionResponse: &llm.FunctionResponse{ID: "", Name: "invalid"}},
					},
				},
			},
		}

		err := cleaner.Transform(ctx, req)
		require.NoError(t, err)
		require.True(t, req.PersistHistory)
		require.Len(t, req.History, 2)
		require.Len(t, req.History[0].Parts, 1)
		require.Equal(t, "valid", req.History[0].Parts[0].FunctionCall.ID)
		require.Len(t, req.History[1].Parts, 1)
		require.Equal(t, "valid", req.History[1].Parts[0].FunctionResponse.ID)
	})

	t.Run("Removes entire message if all parts are invalid", func(t *testing.T) {
		req := &ports.ContextRequest{
			History: []*llm.Content{
				{
					Role: "user",
					Parts: []*llm.Part{
						{Text: "keep me"},
					},
				},
				{
					Role: "model",
					Parts: []*llm.Part{
						{FunctionCall: &llm.FunctionCall{ID: "", Name: "invalid"}},
					},
				},
				{
					Role: "user",
					Parts: []*llm.Part{
						{FunctionResponse: &llm.FunctionResponse{ID: "", Name: "invalid"}},
					},
				},
			},
		}

		err := cleaner.Transform(ctx, req)
		require.NoError(t, err)
		require.True(t, req.PersistHistory)
		require.Len(t, req.History, 1)
		require.Equal(t, "keep me", req.History[0].Parts[0].Text)
	})

	t.Run("No changes if everything is valid", func(t *testing.T) {
		req := &ports.ContextRequest{
			History: []*llm.Content{
				{
					Role: "user",
					Parts: []*llm.Part{
						{Text: "keep me"},
					},
				},
			},
		}

		err := cleaner.Transform(ctx, req)
		require.NoError(t, err)
		require.False(t, req.PersistHistory)
		require.Len(t, req.History, 1)
	})
}
