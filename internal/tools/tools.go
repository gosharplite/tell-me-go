// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
	"github.com/gosharplite/tell-me-go/internal/tools/developer"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
)

// ToolRegistrationParams encapsulates all dependencies for tool registration.
type ToolRegistrationParams struct {
	Registry         tools.IToolRegistry
	SecurityManager  domain_security.ISecurityManager
	CommandExecutor  tools.CommandExecutor
	CommandValidator domain_security.ICommandValidator
	SessionProvider  ports.ISessionProvider
	LogFile          string
	Model            string
	Mode             string
	PricingOverrides map[string]pricing.ModelPricing
	Client           llm.LLMClient
	AssetsDir        string
	EventBus         events.EventBus
	FileSystem       persistence.FileSystem
}

// RegisterAll registers all available tools into the registry.
func RegisterAll(params ToolRegistrationParams) {
	workspace.Register(params.Registry, params.SecurityManager, params.CommandExecutor, params.CommandValidator, params.FileSystem)
	if params.SessionProvider != nil {
		workspace.RegisterPersistence(params.Registry, params.SessionProvider)
	}
	analysis.Register(params.Registry, params.SecurityManager, params.EventBus, params.CommandExecutor, params.FileSystem)
	developer.Register(params.Registry, params.SecurityManager, params.CommandExecutor, params.CommandValidator, params.FileSystem)
	integrations.RegisterAll(params.Registry, params.SecurityManager, params.Client, params.AssetsDir)
}
