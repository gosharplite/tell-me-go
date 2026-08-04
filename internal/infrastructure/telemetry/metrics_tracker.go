// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"log/slog"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// sessionCostTracker manages in-memory cost accumulation to avoid frequent log parsing.
type sessionCostTracker struct {
	mu        sync.Mutex
	stats     domain_pricing.UsageStats
	totalCost float64
	pricing   domain_pricing.PricingData
	model     domain_pricing.ModelPricing
	modelName string
	logFile   string
	mode      string
	sm        domain_security.Manager
	initiated bool

	// currentSessionID is computed once from generateSessionID(t.mode, t.logFile) and set inside ensureInitialized().
	currentSessionID string
}

// NewSessionCostTracker creates a new tracker.
func NewSessionCostTracker(sm domain_security.Manager, logFile string, mode string, modelName string, model domain_pricing.ModelPricing, pricing domain_pricing.PricingData) domain_pricing.CostTracker {
	return &sessionCostTracker{
		sm:        sm,
		logFile:   logFile,
		mode:      mode,
		modelName: modelName,
		model:     model,
		pricing:   pricing,
	}
}

// GetTotalCost returns the accumulated cost.
func (t *sessionCostTracker) GetTotalCost(ctx context.Context) float64 {
	_, totalCost := t.GetStats(ctx)
	return totalCost
}

// GetStats returns the accumulated usage statistics and total cost.
func (t *sessionCostTracker) GetStats(ctx context.Context) (domain_pricing.UsageStats, float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.ensureInitialized()

	return t.stats, t.totalCost
}

// Warmup pre-loads the session state from the log file.
func (t *sessionCostTracker) Warmup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureInitialized()
}

func (t *sessionCostTracker) ensureInitialized() {
	if !t.initiated && t.logFile != "" {
		if usage, totalCost, _, _, err := parseUsage(t.logFile, t.pricing, t.modelName); err == nil {
			t.stats = usage
			t.totalCost = totalCost
		}
		t.initiated = true
		t.currentSessionID = generateSessionID(t.mode, t.logFile)
	}
}

// Accumulate adds new turn metrics to the running total.
func (t *sessionCostTracker) Accumulate(mt llm.Metrics) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.ensureInitialized()

	mtModel := mt.Model
	if mtModel == "" {
		mtModel = t.modelName
	}
	p := t.pricing.GetModelPricing(mtModel)

	turnStats := accumulate(&t.stats, mt)

	calc := &domain_pricing.CostCalculator{Pricing: t.pricing, Model: p}
	t.totalCost += calc.Calculate(turnStats).TotalCost
}

// AccumulateAndReturn adds new turn metrics to the running total and returns the cost for this specific turn.
func (t *sessionCostTracker) AccumulateAndReturn(mt llm.Metrics) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.ensureInitialized()

	mtModel := mt.Model
	if mtModel == "" {
		mtModel = t.modelName
		slog.Debug("metrics model missing, using tracker model", slog.String("model", mtModel))
	} else {
		slog.Debug("using metrics model", slog.String("model", mtModel))
	}

	slog.Debug("token counts",
		slog.Int("prompt", int(mt.PromptTokens)),
		slog.Int("cached", int(mt.CachedTokens)),
		slog.Int("response", int(mt.ResponseTokens)),
		slog.Int("thinking", int(mt.ThinkingTokens)))

	p := t.pricing.GetModelPricing(mtModel)

	slog.Debug("retrieved pricing",
		slog.Float64("hit", p.Hit),
		slog.Float64("miss", p.Miss),
		slog.Float64("comp", p.Comp))

	var dummy domain_pricing.UsageStats
	turnStats := accumulate(&dummy, mt)
	calc := &domain_pricing.CostCalculator{Pricing: t.pricing, Model: p}
	turnCost := calc.Calculate(turnStats).TotalCost

	slog.Debug("calculated turn cost", slog.Float64("cost", turnCost))

	accumulate(&t.stats, mt)
	t.totalCost += turnCost

	return turnCost
}
