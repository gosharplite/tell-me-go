// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
)

func TestRequestBatchConsent_Denied(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{{Name: "dangerous_tool", RequiresConsent: true}}
		},
	}
	sm := &mockConsentSecurityManager{ConfirmResult: false}
	auth := newSecurityAuthorizer(sm, reg, newToolResolutionService(reg))

	calls := []*llm.FunctionCall{{Name: "dangerous_tool"}}
	_, declined := auth.RequestBatchConsent(context.Background(), calls)

	assert.True(t, declined[0], "Expected the tool to be declined by user")
}

func TestIdentifyConsentItems_Panic(t *testing.T) {
	t.Parallel()
	// A registry that panics on GetDeclarations

	reg := &panicRegistry{PanicOnGet: true}
	auth := newSecurityAuthorizer(nil, reg, newToolResolutionService(reg))

	calls := []*llm.FunctionCall{{Name: "any"}}
	indices, declined := auth.IdentifyConsentItems(calls)

	assert.Empty(t, indices)
	assert.True(t, declined[0], "Expected tool to be declined due to panic")
}

func TestAuthorizationPanic(t *testing.T) {
	t.Parallel()
	// Not really a panic in Authorize itself as it stands, but good to have if we add more logic.
	// For now, let's just test basic authorization denial.

	reg := &mockToolRegistry{}
	sm := &mockSecurityManager{AllowedCommands: map[string]bool{"allowed": true}}
	auth := newSecurityAuthorizer(sm, reg, newToolResolutionService(reg))

	err := auth.Authorize(context.Background(), &tools.ToolDeclaration{Name: "forbidden"}, &llm.FunctionCall{Name: "forbidden"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed")
}

func TestAuthorize_AllowsMCPTool(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{}
	sm := &mockSecurityManager{AllowedCommands: map[string]bool{"allowed": true}}
	auth := newSecurityAuthorizer(sm, reg, newToolResolutionService(reg))

	call := &llm.FunctionCall{Name: "mcp_github_run_secret_scanning"}
	tool := &tools.ToolDeclaration{Name: "mcp_github_run_secret_scanning"}

	err := auth.Authorize(context.Background(), tool, call)
	assert.NoError(t, err, "mcp_-prefixed tool names must be allowed by IsToolAllowed")
}

func TestAuthorize_DeniesUnknownNonMCPTool(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{}
	sm := &mockSecurityManager{AllowedCommands: map[string]bool{"allowed": true}}
	auth := newSecurityAuthorizer(sm, reg, newToolResolutionService(reg))

	call := &llm.FunctionCall{Name: "unknown_tool"}
	tool := &tools.ToolDeclaration{Name: "unknown_tool"}

	err := auth.Authorize(context.Background(), tool, call)
	if err == nil {
		t.Fatal("expected error for unknown non-MCP tool")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("error %q does not contain %q", err.Error(), "not allowed")
	}
}

func TestRequestBatchConsent_BypassActive(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{{Name: "dangerous_tool", RequiresConsent: true}}
		},
	}
	sm := &mockConsentSecurityManager{ConfirmResult: true, BypassActive: true}
	auth := newSecurityAuthorizer(sm, reg, newToolResolutionService(reg))

	calls := []*llm.FunctionCall{{Name: "dangerous_tool"}}
	ctx := context.Background()
	_, declined := auth.RequestBatchConsent(ctx, calls)

	assert.False(t, declined[0], "Expected the tool to NOT be declined when bypass is active")
}
