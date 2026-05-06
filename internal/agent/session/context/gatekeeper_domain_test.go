// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/require"
)

func TestTokenGatekeeper_DomainBoundaryValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		history       []*llm.Content
		expectedError error
	}{
		{
			name: "Valid Payload",
			history: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
			},
			expectedError: nil,
		},
		{
			name: "Empty Role",
			history: []*llm.Content{
				{Role: "", Parts: []*llm.Part{{Text: "Hello"}}},
			},
			expectedError: ErrInvalidPayload,
		},
		{
			name: "Nil Parts (not strictly invalid by groupTurns but often problematic)",
			history: []*llm.Content{
				{Role: "user", Parts: nil},
			},
			expectedError: nil, // groupTurns doesn't check parts nil-ness yet, only role.
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gatekeeper := newTokenGatekeeper(
				&agenttest.MockTokenCounter{Tokens: 10},
				nil,
			)

			req := &ports.ContextRequest{
				History:  tt.history,
				Metadata: ports.ContextMetadata{},
			}

			err := gatekeeper.Transform(context.Background(), req)

			if tt.expectedError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.expectedError)
				require.Contains(t, err.Error(), "gatekeeper validation failed")
			}
		})
	}
}
