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
	Registry         tools.Registry
	SecurityManager  domain_security.Manager
	CommandExecutor  tools.CommandExecutor
	CommandValidator domain_security.CommandValidator
	SessionProvider  ports.SessionProvider
	LogFile          string
	TraceFile        string
	Model            string
	Mode             string
	PricingOverrides map[string]pricing.ModelPricing
	Client           llm.LLMClient
	AssetsDir        string
	EventBus         events.EventBus
	FileSystem       persistence.FileSystem
	HealthManager    ports.HealthCheckManager
}

// RegisterAll registers all available tools into the registry.
func RegisterAll(params ToolRegistrationParams) error {
	if err := workspace.Register(params.Registry, params.SecurityManager, params.CommandExecutor, params.CommandValidator, params.FileSystem, params.HealthManager); err != nil {
		return err
	}
	if params.SessionProvider != nil {
		if err := workspace.RegisterPersistence(params.Registry, params.SessionProvider); err != nil {
			return err
		}
	}
	archVerify, err := analysis.Register(params.Registry, params.SecurityManager, params.EventBus, params.CommandExecutor, params.FileSystem)
	if err != nil {
		return err
	}
	if err := developer.Register(params.Registry, params.SecurityManager, params.CommandExecutor, params.CommandValidator, params.FileSystem, archVerify); err != nil {
		return err
	}
	if err := integrations.RegisterAll(params.Registry, params.SecurityManager, params.Client, params.AssetsDir); err != nil {
		return err
	}
	return nil
}
