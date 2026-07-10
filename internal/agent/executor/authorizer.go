// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ToolAuthorizer encapsulates consent and authorization logic for tool execution.
type ToolAuthorizer interface {
	Authorize(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error
	IdentifyConsentItems(calls []*llm.FunctionCall) ([]int, map[int]bool)
	RequestBatchConsent(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool)
}

// ToolAuthService abstracts the security authorization/consent logic.
type securityAuthorizer struct {
	mu       sync.RWMutex
	sm       domain_security.Manager
	registry tools.Registry
	resolver ToolResolutionService
}

// newSecurityAuthorizer creates a new ToolAuthService.
func newSecurityAuthorizer(sm domain_security.Manager, registry tools.Registry, resolver ToolResolutionService) *securityAuthorizer {
	return &securityAuthorizer{
		sm:       sm,
		registry: registry,
		resolver: resolver,
	}
}

func (a *securityAuthorizer) Authorize(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error {
	a.mu.RLock()
	sm := a.sm
	a.mu.RUnlock()

	if sm != nil && !sm.IsCommandAllowed(call.Name) {
		msg := fmt.Sprintf("security policy: command %q is not allowed", call.Name)
		return fmt.Errorf("%s: %w", msg, tools.ErrSecurityPolicy)
	}

	return nil
}

func (a *securityAuthorizer) IdentifyConsentItems(calls []*llm.FunctionCall) ([]int, map[int]bool) {
	declinedMap := make(map[int]bool)
	var consentIndices []int

	for i, call := range calls {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// Fail closed: If we panic evaluating a tool, do not allow it to execute.
					declinedMap[i] = true
				}
			}()

			tool, err := a.resolver.Resolve(call)
			if err == nil && tool.RequiresConsent {
				consentIndices = append(consentIndices, i)
			}
		}()
	}

	return consentIndices, declinedMap
}

func (a *securityAuthorizer) RequestBatchConsent(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
	consentIndices, declinedMap := a.IdentifyConsentItems(calls)

	if len(consentIndices) == 0 {
		return ctx, declinedMap
	}

	a.mu.RLock()
	sm := a.sm
	a.mu.RUnlock()

	if sm != nil {
		if !sm.IsBypassActive() {
			for _, i := range consentIndices {
				declinedMap[i] = true
			}
		}
	}

	return ctx, declinedMap
}
