// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import (
	"context"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Session encapsulates the state of a single conversation session.
type Session struct {
	ID        string
	StartTime time.Time
	History   HistoryManager
}

// NewSession creates a new Session state.
func NewSession(id string, h HistoryManager) *Session {
	return &Session{
		ID:        id,
		StartTime: time.Now(),
		History:   h,
	}
}

// Chatter defines the interface for the AI agent orchestration.
type Chatter interface {
	Chat(ctx context.Context, s *Session, prompt string) error
	SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error
	SetTieredThreshold(ctx context.Context, threshold int) error
	Subscribe(sub func(events.Event))
	Shutdown(ctx context.Context) error
}

// ChatterFactory defines the functional signature for creating a Chatter instance.
type ChatterFactory func(
	loader config.ConfigLoader,
	client llm.LLMGateway,
	hManager HistoryManager,
	registry tools.IToolRegistry,
	sm security.ISecurityManager,
	disableStreaming bool,
	bus events.EventBus,
	model, mode, logPath string,
	pricingOverrides map[string]pricing.ModelPricing,
	tracker pricing.ICostTracker,
) Chatter

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
