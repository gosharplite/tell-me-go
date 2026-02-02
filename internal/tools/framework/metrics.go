// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

// UsageStats holds aggregated token counts for a session.
type UsageStats struct {
	Hits           int64
	Misses         int64
	Comp           int64
	Thinking       int64
	TieredMisses   int64
	TieredComp     int64
	TieredThinking int64
	SearchQueries  int64
}

// CostBreakdown represents the final financial calculation results.
type CostBreakdown struct {
	Stats        UsageStats
	CostHits     float64
	CostMisses   float64
	CostComp     float64
	CostThinking float64
	CostSearch   float64
	TotalCost    float64
}

// CostCalculator handles the financial logic decoupled from IO.
type CostCalculator struct {
	Pricing llm.PricingData
	Model   llm.ModelPricing
}

// SessionCostTracker manages in-memory cost accumulation to avoid frequent log parsing.
type SessionCostTracker struct {
	mu        sync.Mutex
	stats     UsageStats
	totalCost float64
	pricing   llm.PricingData
	model     llm.ModelPricing
	modelName string
	logFile   string
	sm        *security.SecurityManager
	initiated bool
}

// NewSessionCostTracker creates a new tracker.
func NewSessionCostTracker(sm *security.SecurityManager, logFile string, modelName string, model llm.ModelPricing, pricing llm.PricingData) *SessionCostTracker {
	return &SessionCostTracker{
		sm:        sm,
		logFile:   logFile,
		modelName: modelName,
		model:     model,
		pricing:   pricing,
	}
}

// Subscribe registers the tracker to listen for usage metrics events.
func (t *SessionCostTracker) Subscribe(bus events.EventBus) {
	if bus == nil {
		return
	}
	bus.Subscribe(func(e events.Event) {
		if ev, ok := e.(events.UsageMetricsEvent); ok {
			if ev.Metrics != nil {
				t.Accumulate(*ev.Metrics)
			}
		}
	})
}

// GetTotalCost returns the accumulated cost.
func (t *SessionCostTracker) GetTotalCost(ctx context.Context) float64 {
	_, totalCost := t.GetStats(ctx)
	return totalCost
}

// GetStats returns the accumulated usage statistics and total cost.
func (t *SessionCostTracker) GetStats(ctx context.Context) (UsageStats, float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// If not initiated, we do a synchronous warmup as a fallback,
	// but normally this should be triggered by Warmup() early.
	if !t.initiated && t.logFile != "" {
		if usage, totalCost, _, err := ParseUsage(t.logFile, t.pricing, t.modelName); err == nil {
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
		if usage, totalCost, _, err := ParseUsage(t.logFile, t.pricing, t.modelName); err == nil {
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

	Accumulate(&t.stats, mt, p)

	calc := &CostCalculator{Pricing: t.pricing, Model: p}
	t.totalCost += calc.CalculateMetrics(mt).TotalCost
}

// Calculate performs tiered pricing arithmetic.
func (c *CostCalculator) Calculate(stats UsageStats) CostBreakdown {
	cb := CostBreakdown{Stats: stats}
	p := c.Model

	cb.CostHits = float64(stats.Hits) * p.Hit / 1e6
	cb.CostMisses = (float64(stats.Misses)*p.Miss + float64(stats.TieredMisses)*p.TieredMiss) / 1e6
	cb.CostComp = (float64(stats.Comp)*p.Comp + float64(stats.TieredComp)*p.TieredComp) / 1e6
	cb.CostThinking = (float64(stats.Thinking)*p.Comp + float64(stats.TieredThinking)*p.TieredComp) / 1e6
	cb.CostSearch = float64(stats.SearchQueries) * c.Pricing.SearchQuery

	cb.TotalCost = cb.CostHits + cb.CostMisses + cb.CostComp + cb.CostThinking + cb.CostSearch
	return cb
}

// CalculateMetrics calculates the cost for a single metrics entry.
func (c *CostCalculator) CalculateMetrics(mt llm.Metrics) CostBreakdown {
	var stats UsageStats
	Accumulate(&stats, mt, c.Model)
	return c.Calculate(stats)
}

const pricingURL = "https://raw.githubusercontent.com/gosharplite/tell-me-go/main/assets/pricing.json"

// SessionCostRecord represents a single session's financial footprint.
type SessionCostRecord struct {
	Date      string  `json:"date"`
	Session   string  `json:"session"`
	Model     string  `json:"model"`
	TotalCost float64 `json:"total_cost"`
}

var (
	recoveryInProgress sync.Map // historyPath -> bool
	dateRegex          = regexp.MustCompile(`(\d{4})[-_/]?(\d{2})[-_/]?(\d{2})`)
	ledgerMu           sync.Mutex
)

// breakStaleLock removes a lock file if it's older than 5 minutes to prevent deadlocks after crashes.
func breakStaleLock(lockPath string) {
	if info, err := os.Stat(lockPath); err == nil {
		if time.Since(info.ModTime()) > 5*time.Minute {
			_ = os.Remove(lockPath)
		}
	}
}

type metricsManager struct {
	sm               *security.SecurityManager
	metricsMu        sync.Mutex
	logFile          string
	model            string
	mode             string
	pricingOverrides map[string]llm.ModelPricing
}

// RegisterMetrics adds tools for usage and cost analysis to the registry.
func RegisterMetrics(r *registry.Registry, sm *security.SecurityManager, logFile string, model string, mode string, pricingOverrides map[string]llm.ModelPricing) {
	m := &metricsManager{
		sm:               sm,
		logFile:          logFile,
		model:            model,
		mode:             mode,
		pricingOverrides: pricingOverrides,
	}

	r.Register(&tools.ToolDeclaration{
		Name:        "estimate_cost",
		Description: "Calculates the estimated USD cost of the current session.",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		res, err := m.EstimateCost(ctx, true, "") // Records to ledger with default ID
		return tools.ToolResult{Text: res}, err
	})

	r.Register(&tools.ToolDeclaration{
		Name:        "get_cost_summary",
		Description: "Returns a summary of total AI costs grouped by date from the local history ledger.",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		// Silent update: Calculate and record the current session's latest cost before summary.
		_, _ = m.EstimateCost(ctx, true, "")
		res, err := m.getCostSummary(ctx)
		return tools.ToolResult{Text: res}, err
	})
}

// RecordSessionCost calculates and saves the session cost to the global ledger and appends a summary to the log.
func RecordSessionCost(ctx context.Context, sm *security.SecurityManager, tracker *SessionCostTracker, logPath, model, mode, sessionID string, pricingOverrides map[string]llm.ModelPricing) error {
	m := &metricsManager{
		sm:               sm,
		logFile:          logPath,
		model:            model,
		mode:             mode,
		pricingOverrides: pricingOverrides,
	}

	// 1. Record to global ledger (detailed breakdown)
	_, err := m.EstimateCost(ctx, true, sessionID)
	if err != nil {
		return err
	}

	// 2. Append legacy summary to the log file itself
	var usage UsageStats
	var totalCost float64

	if tracker != nil {
		usage, totalCost = tracker.GetStats(ctx)
	} else {
		pricing := GetPricing(ctx, sm, filepath.Dir(logPath))
		for k, v := range pricingOverrides {
			pricing.Models[k] = v
		}

		var err error
		usage, totalCost, _, err = ParseUsage(logPath, pricing, model)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
	}

	if usage.Hits == 0 && usage.Misses == 0 && usage.Comp == 0 && usage.Thinking == 0 &&
		usage.TieredMisses == 0 && usage.TieredComp == 0 && usage.TieredThinking == 0 {
		return nil
	}

	summary := llm.Metrics{
		Timestamp:      time.Now().Format(time.RFC3339),
		Model:          model,
		CachedTokens:   int32(usage.Hits),
		PromptTokens:   int32(usage.Hits + usage.Misses + usage.TieredMisses),
		ResponseTokens: int32(usage.Comp + usage.TieredComp),
		TotalTokens:    int32(usage.Hits + usage.Misses + usage.TieredMisses + usage.Comp + usage.TieredComp + usage.Thinking + usage.TieredThinking),
		ThinkingTokens: int32(usage.Thinking + usage.TieredThinking),
		SearchQueries:  int(usage.SearchQueries),
		Cost:           totalCost,
		IsSummary:      true,
	}

	summaryBytes, _ := json.Marshal(summary)
	fAppend, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer fAppend.Close()

	_, err = fAppend.WriteString(string(summaryBytes) + "\n")
	return err
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
	breakStaleLock(lockPath)
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return
	}
	defer func() {
		lock.Close()
		os.Remove(lockPath)
	}()

	var history []SessionCostRecord

	// 2. Read existing history (or recover if missing)
	if content, err := os.ReadFile(historyPath); err == nil {
		if err := json.Unmarshal(content, &history); err != nil {
			_ = os.Rename(historyPath, historyPath+".bak")
			history = []SessionCostRecord{}
		}
	} else if os.IsNotExist(err) {
		go m.recoverLedger(context.Background(), globalDir)
	}

	// 3. Update or Append (identify by session ID)
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

	// 4. Apply Retention Policy
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

	if retentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -retentionDays).Format("2006-01-02")
		filtered := make([]SessionCostRecord, 0, len(history))
		for _, r := range history {
			if r.Date >= cutoff {
				filtered = append(filtered, r)
			}
		}
		history = filtered
	}

	// 5. Write back atomically
	if bytes, err := json.Marshal(history); err == nil {
		_ = fsutil.AtomicWrite(ctx, historyPath, bytes, 0644)
	}
}

func (m *metricsManager) getCostSummary(ctx context.Context) (string, error) {
	m.metricsMu.Lock()
	defer m.metricsMu.Unlock()

	ledgerMu.Lock()
	defer ledgerMu.Unlock()

	outputDir := filepath.Dir(m.logFile)
	globalDir := filepath.Dir(outputDir)
	historyPath := filepath.Join(globalDir, "global_costs.json")

	// SOP: Auto-recovery of missing ledger
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		go m.recoverLedger(context.Background(), globalDir)
		return "Cost history ledger is missing. Recovery has been started in the background. Please try again in a few moments.", nil
	}

	if _, recovering := recoveryInProgress.Load(historyPath); recovering {
		return "Cost history recovery is currently in progress. Please try again in a few moments.", nil
	}

	content, err := os.ReadFile(historyPath)
	if err != nil {
		return "No cost history found yet. Run 'estimate_cost' to record your first session.", nil
	}

	var history []SessionCostRecord
	if err := json.Unmarshal(content, &history); err != nil {
		return "Error parsing cost history. The file may be corrupted.", err
	}

	// Aggregate by Date
	dailyTotals := make(map[string]float64)
	for _, r := range history {
		dailyTotals[r.Date] += r.TotalCost
	}

	var sb strings.Builder
	sb.WriteString("### AI Usage Cost Summary (by Date)\n\n")
	sb.WriteString("| Date | Total Cost (USD) |\n")
	sb.WriteString("| :--- | :--- |\n")

	// Sort dates descending
	var dates []string
	for d := range dailyTotals {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	var grandTotal float64
	for _, d := range dates {
		cost := dailyTotals[d]
		sb.WriteString(fmt.Sprintf("| %s | $%.4f |\n", d, cost))
		grandTotal += cost
	}
	sb.WriteString(fmt.Sprintf("| **Grand Total** | **$%.4f** |\n", grandTotal))

	return sb.String(), nil
}

// GetPricing handles the tiered fetching of pricing data: Local Cache -> Remote -> Hardcoded Fallback.
func GetPricing(ctx context.Context, sm *security.SecurityManager, outputDir string) llm.PricingData {
	sm.PricingMu().Lock()
	defer sm.PricingMu().Unlock()

	globalDir := outputDir
	// If outputDir is a mode-specific directory (not containing global_prices.json), use parent
	if _, err := os.Stat(filepath.Join(outputDir, "global_prices.json")); os.IsNotExist(err) {
		parent := filepath.Dir(outputDir)
		if _, err := os.Stat(filepath.Join(parent, "global_prices.json")); err == nil {
			globalDir = parent
		}
	}

	cachePath := filepath.Join(globalDir, "global_prices.json")
	var data llm.PricingData
	useCache := false
	isStale := false

	// 1. Try Local Cache
	if info, err := os.Stat(cachePath); err == nil {
		if content, err := os.ReadFile(cachePath); err == nil {
			if err := json.Unmarshal(content, &data); err == nil {
				useCache = true
				if time.Since(info.ModTime()) >= 24*time.Hour {
					isStale = true
				}
			}
		}
	}

	// 2. Try Remote if cache is missing or stale
	if !useCache || isStale {
		if isStale {
			// If we have stale data, return it immediately and fetch in background
			bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			go func() {
				defer cancel()
				fetchAndCachePricing(bgCtx, sm, cachePath)
			}()
			return data
		}

		// Optimization: Check for connectivity before hitting network to avoid long timeout
		client := http.Client{Timeout: 2 * time.Second} // Shorter timeout
		req, _ := http.NewRequestWithContext(ctx, "GET", pricingURL, nil)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				// Save to cache atomically
				if bytes, err := json.MarshalIndent(data, "", "  "); err == nil {
					_ = fsutil.AtomicWrite(ctx, cachePath, bytes, 0644)
				}
				useCache = true
			}
		}
	}

	// 3. Hardcoded Fallback
	if !useCache {
		data = config.DefaultPricing()
	}

	return data
}

func (m *metricsManager) getModelPricing(modelName string, pricing llm.PricingData) llm.ModelPricing {
	return GetModelPricing(modelName, pricing)
}

// GetModelPricing finds the best pricing match for a model name.
func GetModelPricing(modelName string, pricing llm.PricingData) llm.ModelPricing {
	// 1. Exact match
	if p, ok := pricing.Models[modelName]; ok {
		return p
	}
	// 2. Substring match (e.g., "flash", "pro")
	for k, v := range pricing.Models {
		if k != "default" && strings.Contains(modelName, k) {
			return v
		}
	}
	// 3. Fallback to default
	return pricing.Models["default"]
}

func (m *metricsManager) EstimateCost(ctx context.Context, shouldRecord bool, sessionID string) (string, error) {
	resolvedLog, err := m.sm.IsPathSafe(m.logFile)
	if err != nil {
		return "", err
	}

	outputDir := filepath.Dir(resolvedLog)
	pricing := GetPricing(ctx, m.sm, outputDir)

	// Apply overrides from config
	for k, v := range m.pricingOverrides {
		pricing.Models[k] = v
	}

	// 1. Parse usage from log
	usage, totalCost, detectedModel, err := ParseUsage(resolvedLog, pricing, m.model)
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
	p := GetModelPricing(detectedModel, pricing)
	calc := &CostCalculator{Pricing: pricing, Model: p}
	breakdown := calc.Calculate(usage)
	breakdown.TotalCost = totalCost // Use the per-turn accurate total cost

	// 3. Persistence: Record to local ledger
	if shouldRecord {
		if sessionID == "" {
			sessionID = filepath.ToSlash(filepath.Join(m.mode, filepath.Base(m.logFile)))
		}
		m.recordCost(ctx, outputDir, m.mode, SessionCostRecord{
			Date:      time.Now().Format("2006-01-02"),
			Session:   sessionID,
			Model:     detectedModel,
			TotalCost: breakdown.TotalCost,
		})
	}

	// 4. Render report
	return m.renderReport(pricing, breakdown), nil
}

// ParseUsage extracts usage statistics and calculates total cost from a log file.
func ParseUsage(path string, pricing llm.PricingData, defaultModel string) (UsageStats, float64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return UsageStats{}, 0, "", err
	}
	defer f.Close()

	var stats UsageStats
	var totalCost float64
	var detectedModel string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		var mt llm.Metrics

		// Try JSON first (SOP: Structured over Procedural)
		if err := json.Unmarshal([]byte(line), &mt); err == nil {
			if mt.IsSummary {
				continue
			}
			mtModel := mt.Model
			if mtModel == "" {
				mtModel = defaultModel
			}
			if detectedModel == "" && mtModel != "" {
				detectedModel = mtModel
			}

			p := GetModelPricing(mtModel, pricing)
			Accumulate(&stats, mt, p)
			calc := &CostCalculator{Pricing: pricing, Model: p}
			if mt.Cost > 0 {
				totalCost += mt.Cost
			} else {
				totalCost += calc.CalculateMetrics(mt).TotalCost
			}
			continue
		}

		// Fallback to legacy text format parsing
		parts := strings.Fields(line)
		if len(parts) < 15 {
			continue
		}

		h, _ := strconv.ParseInt(parts[2], 10, 64)
		mMiss, _ := strconv.ParseInt(parts[4], 10, 64)
		c, _ := strconv.ParseInt(parts[6], 10, 64)
		s, _ := strconv.ParseInt(parts[12], 10, 64)
		th, _ := strconv.ParseInt(parts[14], 10, 64)

		mtLegacy := llm.Metrics{
			CachedTokens:   int32(h),
			PromptTokens:   int32(h + mMiss),
			ResponseTokens: int32(c),
			SearchQueries:  int(s),
			ThinkingTokens: int32(th),
		}
		if detectedModel == "" {
			detectedModel = defaultModel
		}
		p := GetModelPricing(defaultModel, pricing)
		Accumulate(&stats, mtLegacy, p)
		calc := &CostCalculator{Pricing: pricing, Model: p}
		totalCost += calc.CalculateMetrics(mtLegacy).TotalCost
	}
	return stats, totalCost, detectedModel, scanner.Err()
}

// Accumulate adds metrics to usage statistics.
func Accumulate(stats *UsageStats, mt llm.Metrics, p llm.ModelPricing) {
	h := int64(mt.CachedTokens)
	mMiss := int64(mt.PromptTokens) - h
	if mMiss < 0 {
		mMiss = 0
	}

	stats.Hits += h
	stats.SearchQueries += int64(mt.SearchQueries)

	if p.TieredThreshold > 0 && int64(mt.PromptTokens) > p.TieredThreshold {
		stats.TieredMisses += mMiss
		stats.TieredComp += int64(mt.ResponseTokens)
		stats.TieredThinking += int64(mt.ThinkingTokens)
	} else {
		stats.Misses += mMiss
		stats.Comp += int64(mt.ResponseTokens)
		stats.Thinking += int64(mt.ThinkingTokens)
	}
}

func (m *metricsManager) renderReport(pricing llm.PricingData, breakdown CostBreakdown) string {
	p := m.getModelPricing(m.model, pricing)
	stats := breakdown.Stats

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Estimated Cost for Session (Model: %s):\n", m.model))
	sb.WriteString(fmt.Sprintf("Pricing Data As Of: %s\n", pricing.UpdatedAt))

	// Check for stale data
	if t, err := time.Parse(time.RFC3339, pricing.UpdatedAt); err == nil {
		if time.Since(t) > 30*24*time.Hour {
			sb.WriteString("⚠️ WARNING: Pricing data is over 30 days old. Accuracy not guaranteed.\n")
		}
	} else if pricing.UpdatedAt == "Hardcoded Fallback" {
		sb.WriteString("⚠️ WARNING: Using hardcoded fallback rates. Accuracy not guaranteed.\n")
	}
	sb.WriteString("\n")

	sb.WriteString("| Item | Count | Rate (USD/1M) | Cost (USD) |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- |\n")

	getRateStr := func(item string) string {
		switch item {
		case "hit":
			return fmt.Sprintf("$%.2f", p.Hit)
		case "miss":
			if p.TieredThreshold > 0 {
				return fmt.Sprintf("$%.2f-$%.2f", p.Miss, p.TieredMiss)
			}
			return fmt.Sprintf("$%.2f", p.Miss)
		case "comp":
			if p.TieredThreshold > 0 {
				return fmt.Sprintf("$%.2f-$%.2f", p.Comp, p.TieredComp)
			}
			return fmt.Sprintf("$%.2f", p.Comp)
		case "search":
			return fmt.Sprintf("$%.3f/Q", pricing.SearchQuery)
		}
		return "-"
	}

	sb.WriteString(fmt.Sprintf("| Cache Hits | %d | %s | $%.6f |\n", stats.Hits, getRateStr("hit"), breakdown.CostHits))
	sb.WriteString(fmt.Sprintf("| Cache Misses | %d | %s | $%.6f |\n", stats.Misses+stats.TieredMisses, getRateStr("miss"), breakdown.CostMisses))
	sb.WriteString(fmt.Sprintf("| Completion | %d | %s | $%.6f |\n", stats.Comp+stats.TieredComp, getRateStr("comp"), breakdown.CostComp))
	sb.WriteString(fmt.Sprintf("| Thinking | %d | %s | $%.6f |\n", stats.Thinking+stats.TieredThinking, getRateStr("comp"), breakdown.CostThinking))
	sb.WriteString(fmt.Sprintf("| Search Queries | %d | %s | $%.6f |\n", stats.SearchQueries, getRateStr("search"), breakdown.CostSearch))
	sb.WriteString("| **Total** | | | **$" + fmt.Sprintf("%.4f", breakdown.TotalCost) + "** |\n")

	return sb.String()
}

func fetchAndCachePricing(ctx context.Context, sm *security.SecurityManager, cachePath string) {
	sm.PricingMu().Lock()
	defer sm.PricingMu().Unlock()

	// Double check freshness inside lock
	if info, err := os.Stat(cachePath); err == nil {
		if time.Since(info.ModTime()) < 24*time.Hour {
			return
		}
	}

	client := http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, "GET", pricingURL, nil)
	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		defer resp.Body.Close()
		var data llm.PricingData
		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			if bytes, err := json.MarshalIndent(data, "", "  "); err == nil {
				_ = fsutil.AtomicWrite(ctx, cachePath, bytes, 0644)
			}
		}
	}
}

// recoverLedger crawls backups and mode directories to reconstruct a missing global_costs.json.
func (m *metricsManager) recoverLedger(ctx context.Context, globalDir string) {
	historyPath := filepath.Join(globalDir, "global_costs.json")
	if _, loaded := recoveryInProgress.LoadOrStore(historyPath, true); loaded {
		return
	}
	defer recoveryInProgress.Delete(historyPath)

	var history []SessionCostRecord
	if content, err := os.ReadFile(historyPath); err == nil {
		_ = json.Unmarshal(content, &history)
	}

	seen := make(map[string]bool)
	for _, r := range history {
		seen[r.Session] = true
	}

	// Fetch pricing once for all calculations
	pricing := GetPricing(ctx, m.sm, globalDir)
	for k, v := range m.pricingOverrides {
		pricing.Models[k] = v
	}

	// 1. Walk through all subdirectories
	err := filepath.Walk(globalDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Recovery: error accessing path %q: %v\n", path, err)
			return nil
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), "tokens.log") {
			return nil
		}

		rel, _ := filepath.Rel(globalDir, path)
		rel = filepath.ToSlash(rel)
		sessionID := rel
		if strings.HasPrefix(rel, "backups/") {
			sessionID = "backup/" + rel[len("backups/"):]
		}

		if seen[sessionID] {
			return nil
		}

		_, totalCost, detectedModel, err := ParseUsage(path, pricing, m.model)
		if err == nil {
			modelToUse := detectedModel
			if modelToUse == "" {
				modelToUse = m.model
			}

			// Extract date from path or mod time
			date := info.ModTime().Format("2006-01-02")
			if matches := dateRegex.FindStringSubmatch(rel); len(matches) > 3 {
				date = fmt.Sprintf("%s-%s-%s", matches[1], matches[2], matches[3])
			}

			history = append(history, SessionCostRecord{
				Date:      date,
				Session:   sessionID,
				Model:     modelToUse,
				TotalCost: totalCost,
			})
			seen[sessionID] = true
		} else {
			log.Printf("Recovery: failed to parse %s: %v\n", path, err)
		}
		return nil
	})

	if err != nil {
		log.Printf("Recovery: walk failed: %v\n", err)
	}

	if len(history) > 0 {
		ledgerMu.Lock()
		defer ledgerMu.Unlock()

		lockPath := historyPath + ".lock"
		breakStaleLock(lockPath)
		lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			return // Another process is writing; skip this background update
		}
		defer func() {
			lock.Close()
			os.Remove(lockPath)
		}()

		// Re-read and merge in case of concurrent updates during walk
		if content, err := os.ReadFile(historyPath); err == nil {
			var latest []SessionCostRecord
			if err := json.Unmarshal(content, &latest); err == nil {
				mergedMap := make(map[string]SessionCostRecord)
				for _, r := range history {
					mergedMap[r.Session] = r
				}
				for _, r := range latest {
					mergedMap[r.Session] = r
				}
				history = make([]SessionCostRecord, 0, len(mergedMap))
				for _, r := range mergedMap {
					history = append(history, r)
				}
			}
		}

		if bytes, err := json.Marshal(history); err == nil {
			_ = fsutil.AtomicWrite(ctx, historyPath, bytes, 0644)
		}
	}
}
