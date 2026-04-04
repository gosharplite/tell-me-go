// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"fmt"
	"strings"
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
}

// newSecurityAuthorizer creates a new ToolAuthService.
func newSecurityAuthorizer(sm domain_security.Manager, registry tools.Registry) *securityAuthorizer {
	return &securityAuthorizer{
		sm:       sm,
		registry: registry,
	}
}

func (a *securityAuthorizer) Authorize(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error {
	a.mu.RLock()
	sm := a.sm
	a.mu.RUnlock()

	if sm != nil && !sm.IsCommandAllowed(call.Name) {
		msg := fmt.Sprintf("Error: Security policy: command %q is not allowed", call.Name)
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

			tool, err := resolveTool(a.registry, call)
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

	var sb strings.Builder
	sb.WriteString("The agent requested the following actions requiring approval:\n")
	for idx, i := range consentIndices {
		c := calls[i]
		fmt.Fprintf(&sb, "%d. %s: %v\n", idx+1, c.Name, c.Args)
	}
	sb.WriteString("\nDo you approve all?")

	a.mu.RLock()
	sm := a.sm
	a.mu.RUnlock()

	if sm != nil {
		if !sm.IsBypassActive() {
			sm.TerminalLock()
			approved, _ := sm.Confirm(ctx, sb.String())
			sm.TerminalUnlock()

			if !approved {
				for _, i := range consentIndices {
					declinedMap[i] = true
				}
			} else {
				// BATCH APPROVED: Inject the tool names into the context
				var approvedToolNames []string
				for _, i := range consentIndices {
					approvedToolNames = append(approvedToolNames, calls[i].Name)
				}
				ctx = domain_security.WithApprovedTools(ctx, approvedToolNames)
			}
		}
	}

	return ctx, declinedMap
}
