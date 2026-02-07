// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/pricing"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

// SessionCostTracker manages in-memory cost accumulation to avoid frequent log parsing.
type SessionCostTracker struct {
	mu        sync.Mutex
	stats     pricing.UsageStats
	totalCost float64
	pricing   pricing.PricingData
	model     pricing.ModelPricing
	modelName string
	logFile   string
	mode      string
	sm        security.ISecurityManager
	initiated bool
}

// NewSessionCostTracker creates a new tracker.
func NewSessionCostTracker(sm security.ISecurityManager, logFile string, mode string, modelName string, model pricing.ModelPricing, pricing pricing.PricingData) *SessionCostTracker {
	return &SessionCostTracker{
		sm:        sm,
		logFile:   logFile,
		mode:      mode,
		modelName: modelName,
		model:     model,
		pricing:   pricing,
	}
}

// GetTotalCost returns the accumulated cost.
func (t *SessionCostTracker) GetTotalCost(ctx context.Context) float64 {
	_, totalCost := t.GetStats(ctx)
	return totalCost
}

// GetDailyCost aggregates costs from the global ledger for the current date in UTC-8.
func (t *SessionCostTracker) GetDailyCost(ctx context.Context) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.logFile == "" {
		return t.totalCost
	}

	globalDir := filepath.Dir(filepath.Dir(t.logFile))
	historyPath := filepath.Join(globalDir, "global_costs.json")

	// Synchronize access to the global ledger file.
	// Acquire ledgerMu after t.mu to maintain consistent lock ordering.
	ledgerMu.Lock()
	defer ledgerMu.Unlock()

	// If recovery is in progress, return the in-memory cost to avoid blocking or inconsistent reads.
	if _, recovering := recoveryInProgress.Load(historyPath); recovering {
		return t.totalCost
	}

	content, err := os.ReadFile(historyPath)
	if err != nil {
		return t.totalCost
	}

	var history []SessionCostRecord
	if err := json.Unmarshal(content, &history); err != nil {
		return t.totalCost
	}

	// CRITICAL: Must match the ID generated in EstimateCost
	currentSessionID := generateSessionID(t.mode, t.logFile)

	return t.calculateDailyCost(history, time.Now(), currentSessionID)
}

func (t *SessionCostTracker) calculateDailyCost(records []SessionCostRecord, now time.Time, currentSessionID string) float64 {
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

func (t *SessionCostTracker) getRecordTimestamp(r SessionCostRecord, loc *time.Location) time.Time {
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
func (t *SessionCostTracker) GetStats(ctx context.Context) (pricing.UsageStats, float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// If not initiated, we do a synchronous warmup as a fallback,
	// but normally this should be triggered by Warmup() early.
	if !t.initiated && t.logFile != "" {
		if usage, totalCost, _, _, err := ParseUsage(t.logFile, t.pricing, t.modelName); err == nil {
			t.stats = usage
			t.totalCost = totalCost
		}
		t.initiated = true
	}

	return t.stats, t.totalCost
}

// Warmup pre-loads the session state from the log file.
func (t *SessionCostTracker) Warmup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.initiated && t.logFile != "" {
		if usage, totalCost, _, _, err := ParseUsage(t.logFile, t.pricing, t.modelName); err == nil {
			t.stats = usage
			t.totalCost = totalCost
		}
		t.initiated = true
	}
}

// Accumulate adds new turn metrics to the running total.
func (t *SessionCostTracker) Accumulate(mt llm.Metrics) {
	t.mu.Lock()
	defer t.mu.Unlock()

	mtModel := mt.Model
	if mtModel == "" {
		mtModel = t.modelName
	}
	p := GetModelPricing(mtModel, t.pricing)

	turnStats := Accumulate(&t.stats, mt)

	calc := &pricing.CostCalculator{Pricing: t.pricing, Model: p}
	t.totalCost += calc.Calculate(turnStats).TotalCost
}

// CalculateCost returns the cost of a single metrics entry without accumulating it.
func (t *SessionCostTracker) CalculateCost(mt llm.Metrics) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	mtModel := mt.Model
	if mtModel == "" {
		mtModel = t.modelName
	}
	p := GetModelPricing(mtModel, t.pricing)

	calc := &pricing.CostCalculator{Pricing: t.pricing, Model: p}
	var dummy pricing.UsageStats
	turnStats := Accumulate(&dummy, mt)
	return calc.Calculate(turnStats).TotalCost
}

// AccumulateAndReturn adds new turn metrics to the running total and returns the cost for this specific turn.
func (t *SessionCostTracker) AccumulateAndReturn(mt llm.Metrics) float64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	mtModel := mt.Model
	if mtModel == "" {
		mtModel = t.modelName
	}
	p := GetModelPricing(mtModel, t.pricing)

	var dummy pricing.UsageStats
	turnStats := Accumulate(&dummy, mt)
	calc := &pricing.CostCalculator{Pricing: t.pricing, Model: p}
	turnCost := calc.Calculate(turnStats).TotalCost

	Accumulate(&t.stats, mt)
	t.totalCost += turnCost

	return turnCost
}

type metricsManager struct {
	sm               security.ISecurityManager
	metricsMu        sync.Mutex
	logFile          string
	model            string
	mode             string
	pricingOverrides map[string]pricing.ModelPricing
	ledger           *LedgerStore
}

type costSummaryArgs struct {
	Billing   bool   `json:"billing"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	Interval  string `json:"interval"` // "hour" or "day"
}

type estimateCostArgs struct{}

// RegisterMetrics adds tools for usage and cost analysis to the registry.
func RegisterMetrics(r *registry.Registry, sm security.ISecurityManager, logFile string, model string, mode string, pricingOverrides map[string]pricing.ModelPricing) {
	m := &metricsManager{
		sm:               sm,
		logFile:          logFile,
		model:            model,
		mode:             mode,
		pricingOverrides: pricingOverrides,
		ledger:           NewLedgerStore(sm, model, pricingOverrides),
	}

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "estimate_cost",
		Description: "Calculates the estimated USD cost of the current session.",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		var eArgs estimateCostArgs
		if err := registry.UnmarshalArgs(args, &eArgs); err != nil {
			return tools.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}
		res, err := m.EstimateCost(ctx, true, "") // Records to ledger with default ID
		return tools.ToolResult{Text: res}, err
	}, registry.ToolOptions{Serial: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "get_cost_summary",
		Description: "Returns a summary of total AI costs grouped by date from the local history ledger.",
		Parameters: &tools.Schema{
			Type: "object",
			Properties: map[string]*tools.Schema{
				"billing": {
					Type:        "boolean",
					Description: "If true, aggregates costs using Google Billing timezone (UTC-8).",
				},
				"start_date": {
					Type:        "string",
					Description: "The start date for the summary (YYYY-MM-DD).",
				},
				"end_date": {
					Type:        "string",
					Description: "The end date for the summary (YYYY-MM-DD).",
				},
				"interval": {
					Type:        "string",
					Description: "Aggregation interval: 'hour' or 'day' (default: 'day').",
				},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		var sArgs costSummaryArgs
		if err := registry.UnmarshalArgs(args, &sArgs); err != nil {
			return tools.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
		}

		// Silent update: Calculate and record the current session's latest cost before summary.
		_, _ = m.EstimateCost(ctx, true, "")
		res, err := m.getCostSummary(ctx, sArgs)
		return tools.ToolResult{Text: res}, err
	}, registry.ToolOptions{Serial: true})
}

// RecordSessionCost calculates and saves the session cost to the global ledger and appends a summary to the log.
func RecordSessionCost(ctx context.Context, sm security.ISecurityManager, tracker domain_pricing.ICostTracker, logPath, model, mode, sessionID string, pricingOverrides map[string]pricing.ModelPricing) error {
	m := &metricsManager{
		sm:               sm,
		logFile:          logPath,
		model:            model,
		mode:             mode,
		pricingOverrides: pricingOverrides,
		ledger:           NewLedgerStore(sm, model, pricingOverrides),
	}

	// 1. Record to global ledger (detailed breakdown)
	_, err := m.EstimateCost(ctx, true, sessionID)
	if err != nil {
		return fmt.Errorf("failed to estimate and record session cost: %w", err)
	}

	// 2. Resolve usage stats
	usage, totalCost, err := resolveUsageForSummary(ctx, sm, tracker, logPath, model, pricingOverrides)
	if err != nil {
		return err
	}

	// 3. Append summary to log
	return appendSummaryToLog(logPath, usage, totalCost, model)
}

func resolveUsageForSummary(ctx context.Context, sm security.ISecurityManager, tracker domain_pricing.ICostTracker, logPath, model string, overrides map[string]pricing.ModelPricing) (pricing.UsageStats, float64, error) {
	if tracker != nil {
		usage, totalCost := tracker.GetStats(ctx)
		return usage, totalCost, nil
	}

	pd := GetPricing(ctx, sm, filepath.Dir(logPath))
	for k, v := range overrides {
		pd.Models[k] = v
	}

	usage, totalCost, _, _, err := ParseUsage(logPath, pd, model)
	if err != nil {
		if os.IsNotExist(err) {
			return pricing.UsageStats{}, 0, nil
		}
		return pricing.UsageStats{}, 0, fmt.Errorf("failed to parse usage log for summary: %w", err)
	}
	return usage, totalCost, nil
}

func appendSummaryToLog(logPath string, usage pricing.UsageStats, totalCost float64, model string) error {
	if usage.PromptTokens == 0 && usage.ResponseTokens == 0 && usage.SearchQueries == 0 {
		return nil
	}

	summary := llm.Metrics{
		Timestamp:      time.Now().Format(time.RFC3339),
		Model:          model,
		CachedTokens:   int32(usage.CachedTokens),
		PromptTokens:   int32(usage.PromptTokens),
		ResponseTokens: int32(usage.ResponseTokens),
		TotalTokens:    int32(usage.PromptTokens + usage.ResponseTokens),
		SearchQueries:  int(usage.SearchQueries),
		Cost:           totalCost,
		IsSummary:      true,
	}

	summaryBytes, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("failed to marshal cost summary: %w", err)
	}

	fAppend, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file %q for summary append: %w", logPath, err)
	}
	defer fAppend.Close()

	_, err = fAppend.WriteString(string(summaryBytes) + "\n")
	if err != nil {
		return fmt.Errorf("failed to write cost summary to log: %w", err)
	}
	return nil
}

func (m *metricsManager) recordCost(ctx context.Context, outputDir string, mode string, record SessionCostRecord) {
	m.metricsMu.Lock()
	defer m.metricsMu.Unlock()

	ledgerMu.Lock()
	defer ledgerMu.Unlock()

	// Global costs are in the parent output directory
	globalDir := filepath.Dir(outputDir)
	historyPath := filepath.Join(globalDir, "global_costs.json")
	lockPath := historyPath + ".lock"

	// 1. Acquire simple file-based lock (with stale lock protection)
	lock, err := m.acquireLedgerLock(lockPath)
	if err != nil {
		return
	}
	defer func() {
		lock.Close()
		os.Remove(lockPath)
	}()

	// 2. Update history
	m.updateLedgerHistory(ctx, historyPath, globalDir, outputDir, record)
}

func (m *metricsManager) acquireLedgerLock(lockPath string) (*os.File, error) {
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
	if err != nil && os.IsExist(err) {
		if isStale(lockPath) {
			_ = os.Remove(lockPath)
			lock, err = os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
		}
	}
	return lock, err
}

func (m *metricsManager) triggerLedgerRecovery(ctx context.Context, historyPath, globalDir string) {
	if _, recovering := recoveryInProgress.Load(historyPath); !recovering {
		// Use a background context for recovery so it's not aborted if the request context is cancelled.
		bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerRecoveryTimeout)
		go func() {
			defer cancel()
			m.ledger.RecoverLedger(bgCtx, globalDir)
		}()
	}
}

func (m *metricsManager) updateLedgerHistory(ctx context.Context, historyPath, globalDir, outputDir string, record SessionCostRecord) {
	var history []SessionCostRecord

	// 1. Read existing history (or recover if missing)
	if content, err := os.ReadFile(historyPath); err == nil {
		if err := json.Unmarshal(content, &history); err != nil {
			log.Printf("Warning: Failed to parse ledger %s: %v. Backing up and starting fresh.", historyPath, err)
			_ = os.Rename(historyPath, historyPath+".bak")
			history = []SessionCostRecord{}
		}
	} else if os.IsNotExist(err) && m.ledger != nil {
		m.triggerLedgerRecovery(ctx, historyPath, globalDir)
	}

	// 2. Update or Append (identify by session ID)
	found := false
	for i, r := range history {
		if r.Session == record.Session {
			history[i] = record
			found = true
			break
		}
	}
	if !found {
		history = append(history, record)
	}

	// 3. Apply Retention Policy
	retentionDays := m.loadRetentionDays(outputDir)
	history = m.applyRetentionPolicy(history, retentionDays)

	// 4. Write back atomically
	bytes, err := json.Marshal(history)
	if err != nil {
		log.Printf("Warning: Failed to marshal ledger for %s: %v", historyPath, err)
		return
	}
	_ = fsutil.AtomicWrite(ctx, historyPath, bytes, 0644)
}

func (m *metricsManager) getCostSummary(ctx context.Context, args costSummaryArgs) (string, error) {
	m.metricsMu.Lock()
	defer m.metricsMu.Unlock()

	ledgerMu.Lock()
	defer ledgerMu.Unlock()

	outputDir := filepath.Dir(m.logFile)
	globalDir := filepath.Dir(outputDir)
	historyPath := filepath.Join(globalDir, "global_costs.json")

	history, status, err := m.ensureLedgerReady(ctx, historyPath, globalDir)
	if err != nil {
		return status, err
	}
	if status != "" {
		return status, nil
	}

	intervalTotals, intervalUsage, keys, location, err := m.aggregateCosts(history, args)
	if err != nil {
		return "", err
	}

	return m.formatSummaryTable(args, intervalTotals, intervalUsage, keys, location), nil
}

func (m *metricsManager) getModelPricing(modelName string, pd pricing.PricingData) pricing.ModelPricing {
	return pd.GetModelPricing(modelName)
}

func (m *metricsManager) EstimateCost(ctx context.Context, shouldRecord bool, sessionID string) (string, error) {
	resolvedLog, err := m.sm.IsPathSafe(m.logFile)
	if err != nil {
		return "", err
	}

	outputDir := filepath.Dir(resolvedLog)
	pd := GetPricing(ctx, m.sm, outputDir)

	// Apply overrides from config
	for k, v := range m.pricingOverrides {
		pd.Models[k] = v
	}

	// 1. Parse usage from log
	usage, totalCost, detectedModel, timestamp, err := ParseUsage(resolvedLog, pd, m.model)
	if err != nil {
		if os.IsNotExist(err) {
			return "Error: Log file not found. Ensure you have made at least one request.", nil
		}
		return "", fmt.Errorf("failed to parse usage log: %w", err)
	}

	if detectedModel == "" {
		detectedModel = m.model
	}

	// 2. Delegate financial math to Calculator
	p := GetModelPricing(detectedModel, pd)
	calc := &pricing.CostCalculator{Pricing: pd, Model: p}
	breakdown := calc.Calculate(usage)
	breakdown.TotalCost = totalCost // Use the per-turn accurate total cost

	// 3. Persistence: Record to local ledger
	if shouldRecord {
		if sessionID == "" {
			sessionID = generateSessionID(m.mode, m.logFile)
		}
		if timestamp.IsZero() {
			timestamp = time.Now()
		}
		loc := time.FixedZone("UTC-8", -8*3600)
		m.recordCost(ctx, outputDir, m.mode, SessionCostRecord{
			Date:      timestamp.In(loc).Format("2006-01-02"),
			Timestamp: timestamp,
			Session:   sessionID,
			Model:     detectedModel,
			TotalCost: breakdown.TotalCost,
			Usage:     usage,
		})
	}

	// 4. Render report
	return m.renderReport(pd, breakdown), nil
}

func (m *metricsManager) renderReport(pricing pricing.PricingData, breakdown pricing.CostBreakdown) string {
	p := m.getModelPricing(m.model, pricing)
	stats := breakdown.Stats

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Estimated Cost for Session (Model: %s):\n", m.model))
	sb.WriteString(fmt.Sprintf("Pricing Data As Of: %s\n", pricing.UpdatedAt))
	sb.WriteString("\n")

	sb.WriteString("| SKU | Count | Rate (USD/1M) | Cost (USD) |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- |\n")

	sb.WriteString(fmt.Sprintf("| Text Input | %d | $%.2f | $%.6f |\n", stats.PromptTokens-stats.CachedTokens, p.Miss, breakdown.InputCost))
	sb.WriteString(fmt.Sprintf("| Input Caching | %d | $%.2f | $%.6f |\n", stats.CachedTokens, p.Hit, breakdown.CacheCost))
	sb.WriteString(fmt.Sprintf("| Text Output | %d | $%.2f | $%.6f |\n", stats.ResponseTokens+stats.ThinkingTokens, p.Comp, breakdown.OutputCost))
	sb.WriteString(fmt.Sprintf("| Search Queries | %d | $%.3f/Q | $%.6f |\n", stats.SearchQueries, pricing.SearchQuery, breakdown.SearchCost))
	sb.WriteString("| **Total** | | | **$" + fmt.Sprintf("%.4f", breakdown.TotalCost) + "** |\n")

	return sb.String()
}

func (m *metricsManager) loadRetentionDays(outputDir string) int {
	retentionDays := 30
	configPath := filepath.Join(outputDir, "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		var cfg map[string]string
		if err := json.Unmarshal(data, &cfg); err == nil {
			if val, ok := cfg["cost_retention_days"]; ok {
				if days, err := strconv.Atoi(val); err == nil {
					retentionDays = days
				}
			}
		}
	}
	return retentionDays
}

func (m *metricsManager) applyRetentionPolicy(history []SessionCostRecord, retentionDays int) []SessionCostRecord {
	if retentionDays <= 0 {
		return history
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Format("2006-01-02")
	filtered := make([]SessionCostRecord, 0, len(history))
	for _, r := range history {
		if r.Date >= cutoff {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func (m *metricsManager) ensureLedgerReady(ctx context.Context, historyPath, globalDir string) ([]SessionCostRecord, string, error) {
	// SOP: Auto-recovery of missing ledger
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		m.triggerLedgerRecovery(ctx, historyPath, globalDir)
		return nil, "Cost history ledger is missing. Recovery has been started in the background. Please try again in a few moments.", nil
	}

	if _, recovering := recoveryInProgress.Load(historyPath); recovering {
		return nil, "Cost history recovery is currently in progress. Please try again in a few moments.", nil
	}

	content, err := os.ReadFile(historyPath)
	if err != nil {
		return nil, "No cost history found yet. Run 'estimate_cost' to record your first session.", nil
	}

	var history []SessionCostRecord
	if err := json.Unmarshal(content, &history); err != nil {
		return nil, "Error parsing cost history. The file may be corrupted.", err
	}

	return history, "", nil
}

func (m *metricsManager) getRecordTimestamp(r SessionCostRecord) time.Time {
	ts := r.Timestamp
	if ts.IsZero() {
		var err error
		ts, err = time.Parse("2006-01-02", r.Date)
		if err != nil {
			return time.Time{}
		}
	}
	return ts
}

func (m *metricsManager) parseTimeFilters(args costSummaryArgs, loc *time.Location) (time.Time, time.Time, error) {
	var startFilter, endFilter time.Time
	if args.StartDate != "" {
		var err error
		startFilter, err = time.ParseInLocation("2006-01-02", args.StartDate, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid start_date format (use YYYY-MM-DD): %w", err)
		}
	}
	if args.EndDate != "" {
		end, err := time.ParseInLocation("2006-01-02", args.EndDate, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid end_date format (use YYYY-MM-DD): %w", err)
		}
		endFilter = end.Add(24 * time.Hour) // Make end date inclusive of the full day
	}
	return startFilter, endFilter, nil
}

func (m *metricsManager) aggregateHistory(history []SessionCostRecord, start, end time.Time, loc *time.Location, format string) (map[string]float64, map[string]pricing.UsageStats) {
	intervalTotals := make(map[string]float64)
	intervalUsage := make(map[string]pricing.UsageStats)

	for _, r := range history {
		ts := m.getRecordTimestamp(r)
		if ts.IsZero() {
			continue
		}

		// Apply range filter
		if !start.IsZero() && ts.Before(start) {
			continue
		}
		if !end.IsZero() && !ts.Before(end) {
			continue
		}

		// Determine the key for aggregation
		effectiveKey := ts.In(loc).Format(format)

		intervalTotals[effectiveKey] += r.TotalCost
		u := intervalUsage[effectiveKey]
		u.PromptTokens += r.Usage.PromptTokens
		u.ResponseTokens += r.Usage.ResponseTokens
		u.CachedTokens += r.Usage.CachedTokens
		u.ThinkingTokens += r.Usage.ThinkingTokens
		intervalUsage[effectiveKey] = u
	}
	return intervalTotals, intervalUsage
}

func (m *metricsManager) aggregateCosts(history []SessionCostRecord, args costSummaryArgs) (map[string]float64, map[string]pricing.UsageStats, []string, *time.Location, error) {
	location := time.Local
	if args.Billing {
		location = time.FixedZone("UTC-8", -8*3600)
	}

	start, end, err := m.parseTimeFilters(args, location)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	format := "2006-01-02"
	switch args.Interval {
	case "", "day":
		// use default
	case "hour":
		format = "2006-01-02 15:00"
	default:
		return nil, nil, nil, nil, fmt.Errorf("invalid interval %q: must be 'day' or 'hour'", args.Interval)
	}

	intervalTotals, intervalUsage := m.aggregateHistory(history, start, end, location, format)

	// Sort keys descending
	var keys []string
	for k := range intervalTotals {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))

	return intervalTotals, intervalUsage, keys, location, nil
}

func (m *metricsManager) formatSummaryTable(args costSummaryArgs, intervalTotals map[string]float64, intervalUsage map[string]pricing.UsageStats, keys []string, location *time.Location) string {
	title := "AI Usage Cost Summary (by Date)"
	if args.Interval == "hour" {
		title = "AI Usage Cost Summary (by Hour)"
	}
	if args.Billing {
		title += " - Google Billing Cycle (UTC-8)"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("### %s\n\n", title))
	headerName := "Date"
	if args.Interval == "hour" {
		headerName = "Date/Hour"
	}
	sb.WriteString(fmt.Sprintf("| %s | Miss | Hit | Other | Eff %% | Total Cost (USD) |\n", headerName))
	sb.WriteString("| :--- | :--- | :--- | :--- | :--- | :--- |\n")

	var grandTotal float64
	var totalM, totalH, totalO int64
	for _, k := range keys {
		cost := intervalTotals[k]
		u := intervalUsage[k]

		mTokens := u.PromptTokens - u.CachedTokens
		hTokens := u.CachedTokens
		oTokens := u.ResponseTokens + u.ThinkingTokens
		eff := 0.0
		if total := mTokens + hTokens; total > 0 {
			eff = float64(hTokens) / float64(total) * 100
		}

		sb.WriteString(fmt.Sprintf("| %s | %d | %d | %d | %.1f%% | $%.4f |\n", k, mTokens, hTokens, oTokens, eff, cost))
		grandTotal += cost
		totalM += mTokens
		totalH += hTokens
		totalO += oTokens
	}

	totalEff := 0.0
	if total := totalM + totalH; total > 0 {
		totalEff = float64(totalH) / float64(total) * 100
	}
	sb.WriteString(fmt.Sprintf("| **Grand Total** | **%d** | **%d** | **%d** | **%.1f%%** | **$%.4f** |\n", totalM, totalH, totalO, totalEff, grandTotal))

	return sb.String()
}

// generateSessionID creates a unique identifier for a session based on its mode and log file name.
// This ID is used as the unique key in global_costs.json to identify and update session records.
func generateSessionID(mode, logFile string) string {
	return filepath.ToSlash(filepath.Join(mode, filepath.Base(logFile)))
}
