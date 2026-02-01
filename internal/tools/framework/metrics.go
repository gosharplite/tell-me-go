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

	f, err := os.Open(resolvedLog)
	if err != nil {
		return "Error: Log file not found. Ensure you have made at least one request.", nil
	}
	defer f.Close()

	var totalH, totalM, totalC, totalS, totalTh int64
	var costH, costM, costC, costTh, costS float64

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) < 15 {
			continue
		}

		h, _ := strconv.ParseInt(parts[2], 10, 64)
		mMiss, _ := strconv.ParseInt(parts[4], 10, 64)
		c, _ := strconv.ParseInt(parts[6], 10, 64)
		s, _ := strconv.ParseInt(parts[12], 10, 64)
		th, _ := strconv.ParseInt(parts[14], 10, 64)

		totalH += h
		totalM += mMiss
		totalC += c
		totalS += s
		totalTh += th

		// Pricing Selection
		p := m.getModelPricing(m.model, pricing)

		rh, rm, rc := p.Hit, p.Miss, p.Comp
		if p.TieredThreshold > 0 && (h+mMiss) > p.TieredThreshold {
			rm, rc = p.TieredMiss, p.TieredComp
		}

		costH += (float64(h) * rh / 1e6)
		costM += (float64(mMiss) * rm / 1e6)
		costC += (float64(c) * rc / 1e6)
		costTh += (float64(th) * rc / 1e6)
		costS += float64(s) * pricing.SearchQuery
	}

	totalCost := costH + costM + costC + costTh + costS

	// Persistence: Record to local ledger
	if shouldRecord {
		if sessionID == "" {
			sessionID = filepath.Base(m.logFile)
		}
		m.recordCost(ctx, outputDir, m.mode, SessionCostRecord{
			Date:      time.Now().Format("2006-01-02"),
			Session:   sessionID,
			Model:     m.model,
			TotalCost: totalCost,
		})
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Estimated Cost for Session (Model: %s):\n", m.model))
	sb.WriteString(fmt.Sprintf("Pricing Data As Of: %s\n", pricing.UpdatedAt))

	// Check for stale data (older than 30 days)
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

	// Helper to determine display rate (shows tiered if applicable)
	getRateStr := func(item string) string {
		p := m.getModelPricing(m.model, pricing)

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

	sb.WriteString(fmt.Sprintf("| Cache Hits | %d | %s | $%.6f |\n", totalH, getRateStr("hit"), costH))
	sb.WriteString(fmt.Sprintf("| Cache Misses | %d | %s | $%.6f |\n", totalM, getRateStr("miss"), costM))
	sb.WriteString(fmt.Sprintf("| Completion | %d | %s | $%.6f |\n", totalC, getRateStr("comp"), costC))
	sb.WriteString(fmt.Sprintf("| Thinking | %d | %s | $%.6f |\n", totalTh, getRateStr("comp"), costTh))
	sb.WriteString(fmt.Sprintf("| Search Queries | %d | %s | $%.6f |\n", totalS, getRateStr("search"), costS))
	sb.WriteString("| **Total** | | | **$" + fmt.Sprintf("%.4f", totalCost) + "** |\n")

	return sb.String(), nil
}
