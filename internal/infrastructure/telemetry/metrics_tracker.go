// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

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

	// cachedExternalDailyCost holds the sum of today's other-session costs from the global ledger.
	cachedExternalDailyCost float64
	// cachedDate is the UTC-8 date ("2006-01-02") the cache is valid for; zero-value "" triggers refresh on first use.
	cachedDate string
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

// GetDailyCost aggregates costs from the global ledger for the current date in UTC-8.
// Uses a cached external cost to avoid synchronous disk I/O on every invocation.
// The cache is refreshed when the UTC-8 date changes.
func (t *sessionCostTracker) GetDailyCost(ctx context.Context) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.logFile == "" {
		return t.totalCost
	}

	now := time.Now()
	loc := time.FixedZone("UTC-8", -8*3600)
	today := now.In(loc).Format("2006-01-02")

	if t.cachedDate != today {
		t.refreshExternalDailyCache(now)
	}

	return t.cachedExternalDailyCost + t.totalCost
}

// refreshExternalDailyCache reads the global ledger and caches the
// external (other-session) cost for today's UTC-8 date.
// Must be called with t.mu held.
func (t *sessionCostTracker) refreshExternalDailyCache(now time.Time) {
	loc := time.FixedZone("UTC-8", -8*3600)
	today := now.In(loc).Format("2006-01-02")

	// Always set cachedDate so we don't retry on every call after a failure.
	t.cachedDate = today

	if t.logFile == "" {
		t.cachedExternalDailyCost = 0
		return
	}

	globalDir := filepath.Dir(filepath.Dir(t.logFile))
	historyPath := filepath.Join(globalDir, "global_costs.json")

	ledgerMu.Lock()
	defer ledgerMu.Unlock()

	if _, recovering := recoveryInProgress.Load(historyPath); recovering {
		t.cachedExternalDailyCost = 0
		return
	}

	content, err := os.ReadFile(historyPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Warning: Failed to read global ledger at %s: %v", historyPath, err)
		}
		t.cachedExternalDailyCost = 0
		return
	}

	var history []sessionCostRecord
	if err := json.Unmarshal(content, &history); err != nil {
		log.Printf("Warning: Failed to parse global ledger at %s: %v", historyPath, err)
		t.cachedExternalDailyCost = 0
		return
	}

	if t.currentSessionID == "" {
		t.currentSessionID = generateSessionID(t.mode, t.logFile)
	}
	// calculateDailyCost returns external + t.totalCost, so subtract to get external-only.
	fullDaily := t.calculateDailyCost(history, now, t.currentSessionID)
	t.cachedExternalDailyCost = fullDaily - t.totalCost
}

func (t *sessionCostTracker) calculateDailyCost(records []sessionCostRecord, now time.Time, currentSessionID string) float64 {
	loc := time.FixedZone("UTC-8", -8*3600)
	today := now.In(loc).Format("2006-01-02")

	var dailyTotal float64

	for _, r := range records {
		ts := t.getRecordTimestamp(r, loc)
		if ts.IsZero() {
			continue
		}

		if ts.In(loc).Format("2006-01-02") == today {
			if r.Session != currentSessionID {
				dailyTotal += r.TotalCost
			}
		}
	}

	// Always add the current session's in-memory cost as it is the Source of Truth for its own footprint.
	// This ensures that even if the session isn't in the ledger yet, or if the ledger has a stale value,
	// the daily total reflects the latest known cost.
	dailyTotal += t.totalCost

	return dailyTotal
}

func (t *sessionCostTracker) getRecordTimestamp(r sessionCostRecord, loc *time.Location) time.Time {
	ts := r.Timestamp
	if ts.IsZero() {
		var err error
		ts, err = time.ParseInLocation("2006-01-02", r.Date, loc)
		if err != nil {
			return time.Time{}
		}
	}
	return ts
}

// GetStats returns the accumulated usage statistics and total cost.
func (t *sessionCostTracker) GetStats(ctx context.Context) (domain_pricing.UsageStats, float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.ensureInitialized()

	return t.stats, t.totalCost
}

// Warmup pre-loads the session state from the log file and caches
// the external daily cost from the global ledger.
func (t *sessionCostTracker) Warmup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ensureInitialized()
	if t.logFile != "" && t.cachedDate == "" {
		t.refreshExternalDailyCache(time.Now())
	}
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
	p := GetModelPricing(mtModel, t.pricing)

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

	p := GetModelPricing(mtModel, t.pricing)

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
