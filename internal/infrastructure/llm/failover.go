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
//
// Separation of concerns:
//
//	ResilientClient (wraps each provider) — single-provider retry, auth refresh,
//	connection resetting, and raw error → domain sentinel classification via
//	llmerr.Classify. Every client in the failover chain MUST be wrapped in
//	ResilientClient (this is enforced by newFailoverChain in factory.go).
//
//	FailoverGateway (this type) — provider iteration and routing decisions based
//	solely on the domain sentinels returned by ResilientClient. It does NOT
//	repeat error classification; it only checks IsTransient() to decide whether
//	to try the next provider or abort the chain.
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
// Each client in the chain is expected to return domain-classified errors
// (via ResilientClient / llmerr.Classify). FailoverGateway only routes
// based on the error category:
//   - On success: sets metrics.Provider to the client's name and returns.
//   - On transient (including rate limits): records the error and tries the
//     next provider in the chain.
//   - On any non-transient error (auth, terminal, unrecognized): aborts
//     immediately — no further providers are tried.
//   - If all providers are exhausted: returns the last error wrapped as terminal.
func (fg *FailoverGateway) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	var lastErr error
	for _, nc := range fg.clients {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("failover: context cancelled before provider %q: %w", nc.Name, err)
		}

		content, metrics, err := nc.Client.Generate(ctx, input, tools, resolver)
		if err == nil {
			if metrics != nil {
				metrics.Provider = nc.Name
			}
			return content, metrics, nil
		}

		// ResilientClient already classified this error as a domain sentinel
		// (ErrAuth / ErrTransient / ErrRateLimit / ErrTerminal) via llmerr.Classify.
		// FailoverGateway only decides whether to retry the next provider.
		if !llm.IsTransient(err) {
			// Auth, terminal, context-limit, and any unclassified errors abort the chain.
			return nil, nil, fmt.Errorf("failover: provider %q: %w", nc.Name, err)
		}

		// IsTransient covers both ErrTransient and ErrRateLimit — try next provider.
		lastErr = err
		continue
	}

	// All providers exhausted — wrap last error as terminal
	return nil, nil, fmt.Errorf("failover: all %d providers exhausted: %w: %w",
		len(fg.clients), lastErr, llm.ErrTerminal)
}

// SendChat delegates to the primary client.
func (fg *FailoverGateway) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("failover: context cancelled: %w", err)
	}

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
