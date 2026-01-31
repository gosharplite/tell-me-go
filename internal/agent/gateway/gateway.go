// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gateway

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/types"
)

// LLMGateway defines the interface for resilient AI model interactions.
type LLMGateway interface {
	// Generate handles auth retries and establishes the stream.
	Generate(ctx context.Context, input []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver) (*types.Content, *types.Metrics, error)
}
