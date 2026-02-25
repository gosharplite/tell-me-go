// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
)

func TestRequestBatchConsent_Denied(t *testing.T) {
	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{{Name: "dangerous_tool", RequiresConsent: true}}
		},
	}
	sm := &mockConsentSecurityManager{confirmResult: false}
	auth := newSecurityAuthorizer(sm, reg)

	calls := []*llm.FunctionCall{{Name: "dangerous_tool"}}
	declined := auth.RequestBatchConsent(context.Background(), calls)

	assert.True(t, declined[0], "Expected the tool to be declined by user")
}

func TestIdentifyConsentItems_Panic(t *testing.T) {
	// A registry that panics on GetDeclarations
	reg := &panicRegistry{panicOnGet: true}
	auth := newSecurityAuthorizer(nil, reg)

	calls := []*llm.FunctionCall{{Name: "any"}}
	indices, declined := auth.IdentifyConsentItems(calls)

	assert.Empty(t, indices)
	assert.True(t, declined[0], "Expected tool to be declined due to panic")
}

func TestAuthorizationPanic(t *testing.T) {
	// Not really a panic in AuthorizeTool itself as it stands, but good to have if we add more logic.
	// For now, let's just test basic authorization denial.
	reg := &mockToolRegistry{}
	sm := &mockSecurityManager{allowedCommands: map[string]bool{"allowed": true}}
	auth := newSecurityAuthorizer(sm, reg)

	err := auth.AuthorizeTool(&tools.ToolDeclaration{Name: "forbidden"}, &llm.FunctionCall{Name: "forbidden"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}
