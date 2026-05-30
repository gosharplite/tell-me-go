// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// NamedClient pairs an ExtendedClient with a human-readable name for observability.
type NamedClient struct {
	Name   string
	Client llm.ExtendedClient
}

// FailoverGateway implements llm.ExtendedClient by iterating through an ordered
// list of provider clients on Generate, falling through on transient errors.
// Non-Generate methods (SendChat, GenerateImages, RefreshAuth) delegate to the
// primary (first) client.
type FailoverGateway struct {
	clients []NamedClient // clients[0] is primary
}

// compile-time interface compliance check
var _ llm.ExtendedClient = (*FailoverGateway)(nil)

// NewFailoverGateway creates a FailoverGateway backed by the given clients.
// Panics if clients is empty — this is a programmer error that must be caught at startup.
func NewFailoverGateway(clients []NamedClient) *FailoverGateway {
	if len(clients) == 0 {
		panic("FailoverGateway: clients must not be empty")
	}
	return &FailoverGateway{
		clients: clients,
	}
}

// Generate implements the core failover logic.
//
// It iterates through fg.clients in order:
//   - On success: sets metrics.Provider to the client's name and returns.
//   - On auth or terminal error: wraps and returns immediately (no fallback).
//   - On transient error (including rate limits): records the error and tries
//     the next provider.
//   - If all providers are exhausted: returns the last error wrapped as terminal.
func (fg *FailoverGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	var lastErr error
	for _, nc := range fg.clients {
		content, metrics, err := nc.Client.Generate(ctx, input, tools, resolver)
		if err == nil {
			if metrics != nil {
				metrics.Provider = nc.Name
			}
			return content, metrics, nil
		}

		if llm.IsAuth(err) || llm.IsTerminal(err) {
			return nil, nil, fmt.Errorf("failover: provider %q: %w", nc.Name, err)
		}

		// IsTransient covers both ErrTransient and ErrRateLimit
		if llm.IsTransient(err) {
			lastErr = err
			continue
		}

		// Any unrecognised error is treated as terminal
		return nil, nil, fmt.Errorf("failover: provider %q: %w", nc.Name, err)
	}

	// All providers exhausted — wrap last error as terminal
	return nil, nil, fmt.Errorf("failover: all %d providers exhausted: %w: %w",
		len(fg.clients), lastErr, llm.ErrTerminal)
}

// SendChat delegates to the primary client.
func (fg *FailoverGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return fg.clients[0].Client.SendChat(ctx, history, tools, resolver)
}

// GenerateImages delegates to the primary client.
func (fg *FailoverGateway) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	return fg.clients[0].Client.GenerateImages(ctx, model, prompt, mimeType)
}

// RefreshAuth delegates to the primary client.
func (fg *FailoverGateway) RefreshAuth() error {
	return fg.clients[0].Client.RefreshAuth()
}
