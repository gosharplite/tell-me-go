// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/telemetry"
	"github.com/gosharplite/tell-me-go/internal/tools/analysis"
	"github.com/gosharplite/tell-me-go/internal/tools/developer"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
)

// RegisterAll registers all available tools into the registry.
func RegisterAll(
	r *registry.Registry,
	sm *security.SecurityManager,
	outputDir string,
	logFile string,
	model string,
	mode string,
	pricingOverrides map[string]pricing.ModelPricing,
	client llm.LLMClient,
	assetsDir string,
	bus events.EventBus,
) {
	ctx := context.Background()
	state, _ := persistence.NewSessionState(ctx, outputDir)

	executor := &exec.RealExecutor{}
	workspace.Register(r, sm, executor)
	if state != nil {
		workspace.NewPersistenceTools(state).Register(r)
	}
	security.RegisterPolicy(r, sm)
	telemetry.RegisterMetrics(r, sm, logFile, model, mode, pricingOverrides)
	analysis.Register(r, sm, bus)
	developer.Register(r, sm, executor)
	integrations.RegisterAll(r, sm, client, assetsDir)
}
