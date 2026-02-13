// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
	"github.com/gosharplite/tell-me-go/internal/tools/developer"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
)

// RegisterAll registers all available tools into the registry.
func RegisterAll(
	r tools.IToolRegistry,
	sm domain_security.ISecurityManager,
	executor tools.CommandExecutor,
	validator domain_security.ICommandValidator,
	state services.ISessionProvider,
	logFile string,
	model string,
	mode string,
	pricingOverrides map[string]pricing.ModelPricing,
	client llm.LLMClient,
	assetsDir string,
	bus events.EventBus,
) {
	workspace.Register(r, sm, executor, validator)
	if state != nil {
		workspace.RegisterPersistence(r, state)
	}
	analysis.Register(r, sm, bus, executor)
	developer.Register(r, sm, executor, validator)
	integrations.RegisterAll(r, sm, client, assetsDir)
}
