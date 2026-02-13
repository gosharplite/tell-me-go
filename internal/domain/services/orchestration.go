// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// SessionConfig defines the configuration interface for a session.
type SessionConfig interface {
	GetPrompt() string
	GetLastN() int
	GetRawOutput() bool
	GetConfig() *config.Config
}

// SessionDependencies defines the dependencies required for a session.
type SessionDependencies interface {
	GetGateway() llm.LLMGateway
	GetHistoryManager() HistoryManager
	GetRegistry() tools.IToolRegistry
	GetEventBus() events.EventBus
	GetPaths() *persistence.Paths
	GetPricingOverrides() map[string]pricing.ModelPricing
	GetTracker() pricing.ICostTracker
	GetPricingData() pricing.PricingData
}
