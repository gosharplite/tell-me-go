// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

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

const pricingURL = "https://raw.githubusercontent.com/gosharplite/tell-me-go/main/assets/pricing.json"

// SessionCostRecord represents a single session's financial footprint.
type SessionCostRecord struct {
	Date      string  `json:"date"`
	Session   string  `json:"session"`
	Model     string  `json:"model"`
	TotalCost float64 `json:"total_cost"`
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
func RecordSessionCost(ctx context.Context, sm *security.SecurityManager, logPath, model, mode, sessionID string, pricingOverrides map[string]llm.ModelPricing) error {
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
	f, err := os.OpenFile(logPath, os.O_RDONLY, 0644)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	var totalCached, totalPrompt, totalResponse int32
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var mt llm.Metrics
		if err := json.Unmarshal([]byte(scanner.Text()), &mt); err == nil {
			totalCached += mt.CachedTokens
			totalPrompt += mt.PromptTokens
			totalResponse += mt.ResponseTokens
		}
	}

	if totalCached == 0 && totalPrompt == 0 && totalResponse == 0 {
		return nil
	}

	// Fetch pricing for summary
	pricing := GetPricing(ctx, sm, filepath.Dir(logPath))
	// Apply overrides
	for k, v := range pricingOverrides {
		pricing.Models[k] = v
	}
	p := m.getModelPricing(model, pricing)

	// Simple calculation for summary (doesn't account for tiered/thinking here, but it's just a legacy summary)
	cost := (float64(totalCached) * p.Hit / 1e6) +
		(float64(totalPrompt) * p.Miss / 1e6) +
		(float64(totalResponse) * p.Comp / 1e6)

	summary := llm.Metrics{
		Timestamp:      time.Now().Format(time.RFC3339),
		CachedTokens:   totalCached,
		PromptTokens:   totalPrompt,
		ResponseTokens: totalResponse,
		TotalTokens:    totalCached + totalPrompt + totalResponse,
		Duration:       cost, // We repurpose Duration to store USD cost in summary entries
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

	// Global costs are in the parent output directory
	globalDir := filepath.Dir(outputDir)
	historyPath := filepath.Join(globalDir, "global_costs.json")
	lockPath := historyPath + ".lock"

	// 1. Acquire simple file-based lock
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return
	}
	defer func() {
		lock.Close()
		os.Remove(lockPath)
	}()

	var history []SessionCostRecord

	// 2. Read existing history
	if content, err := os.ReadFile(historyPath); err == nil {
		if err := json.Unmarshal(content, &history); err != nil {
			_ = os.Rename(historyPath, historyPath+".bak")
			history = []SessionCostRecord{}
		}
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

	outputDir := filepath.Dir(m.logFile)
	globalDir := filepath.Dir(outputDir)
	historyPath := filepath.Join(globalDir, "global_costs.json")
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

	// 1. Try Local Cache
	if info, err := os.Stat(cachePath); err == nil {
		if time.Since(info.ModTime()) < 24*time.Hour {
			if content, err := os.ReadFile(cachePath); err == nil {
				if err := json.Unmarshal(content, &data); err == nil {
					useCache = true
				}
			}
		}
	}

	// 2. Try Remote if cache is missing or stale
	if !useCache {
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

	p := m.getModelPricing(m.model, pricing)

	// 1. Parse usage from log
	usage, err := m.parseUsage(resolvedLog, p)
	if err != nil {
		if os.IsNotExist(err) {
			return "Error: Log file not found. Ensure you have made at least one request.", nil
		}
		return "", fmt.Errorf("failed to parse usage log: %w", err)
	}

	// 2. Delegate financial math to Calculator
	calc := &CostCalculator{Pricing: pricing, Model: p}
	breakdown := calc.Calculate(usage)

	// 3. Persistence: Record to local ledger
	if shouldRecord {
		if sessionID == "" {
			sessionID = filepath.Base(m.logFile)
		}
		m.recordCost(ctx, outputDir, m.mode, SessionCostRecord{
			Date:      time.Now().Format("2006-01-02"),
			Session:   sessionID,
			Model:     m.model,
			TotalCost: breakdown.TotalCost,
		})
	}

	// 4. Render report
	return m.renderReport(pricing, breakdown), nil
}

func (m *metricsManager) parseUsage(path string, p llm.ModelPricing) (UsageStats, error) {
	f, err := os.Open(path)
	if err != nil {
		return UsageStats{}, err
	}
	defer f.Close()

	var stats UsageStats
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		var mt llm.Metrics

		// Try JSON first (SOP: Structured over Procedural)
		if err := json.Unmarshal([]byte(line), &mt); err == nil {
			m.accumulate(&stats, mt, p)
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
		m.accumulate(&stats, mtLegacy, p)
	}
	return stats, scanner.Err()
}

func (m *metricsManager) accumulate(stats *UsageStats, mt llm.Metrics, p llm.ModelPricing) {
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
