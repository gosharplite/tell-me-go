// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	PromptTokens   int64
	ResponseTokens int64
	CachedTokens   int64
	SearchQueries  int64
	ThinkingTokens int64
}

// CostBreakdown represents the final financial calculation results.
type CostBreakdown struct {
	Stats      UsageStats
	InputCost  float64
	CacheCost  float64
	OutputCost float64
	SearchCost float64
	TotalCost  float64
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

// Calculate performs pricing arithmetic based on Vertex AI SKUs.
func (c *CostCalculator) Calculate(stats UsageStats) CostBreakdown {
	cb := CostBreakdown{Stats: stats}
	p := c.Model

	inputTokens := stats.PromptTokens - stats.CachedTokens
	if inputTokens < 0 {
		inputTokens = 0
	}

	cb.InputCost = float64(inputTokens) * p.Miss / 1e6
	cb.CacheCost = float64(stats.CachedTokens) * p.Hit / 1e6
	cb.OutputCost = float64(stats.ResponseTokens+stats.ThinkingTokens) * p.Comp / 1e6
	cb.SearchCost = float64(stats.SearchQueries) * c.Pricing.SearchQuery

	cb.TotalCost = cb.InputCost + cb.CacheCost + cb.OutputCost + cb.SearchCost
	return cb
}

// CalculateMetrics calculates the cost for a single metrics entry.
func (c *CostCalculator) CalculateMetrics(mt llm.Metrics) CostBreakdown {
	var stats UsageStats
	Accumulate(&stats, mt, c.Model)
	return c.Calculate(stats)
}

// SessionCostRecord represents a single session's financial footprint.
type SessionCostRecord struct {
	Date      string     `json:"date"`
	Session   string     `json:"session"`
	Model     string     `json:"model"`
	TotalCost float64    `json:"total_cost"`
	Usage     UsageStats `json:"usage,omitempty"`
}

var (
	recoveryInProgress sync.Map // historyPath -> bool
	dateRegex          = regexp.MustCompile(`(\d{4})[-_/]?(\d{2})[-_/]?(\d{2})`)
	ledgerMu           sync.Mutex
)

const ledgerRecoveryTimeout = 10 * time.Minute

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
		if _, recovering := recoveryInProgress.Load(historyPath); !recovering {
			// Use a background context for recovery so it's not aborted if the request context is cancelled.
			bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerRecoveryTimeout)
			go func() {
				defer cancel()
				m.recoverLedger(bgCtx, globalDir)
			}()
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

	ledgerMu.Lock()
	defer ledgerMu.Unlock()

	outputDir := filepath.Dir(m.logFile)
	globalDir := filepath.Dir(outputDir)
	historyPath := filepath.Join(globalDir, "global_costs.json")

	// SOP: Auto-recovery of missing ledger
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		if _, recovering := recoveryInProgress.Load(historyPath); !recovering {
			// Use a background context for recovery so it's not aborted if the request context is cancelled.
			bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerRecoveryTimeout)
			go func() {
				defer cancel()
				m.recoverLedger(bgCtx, globalDir)
			}()
		}
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
	dailyUsage := make(map[string]UsageStats) // Track usage per day
	for _, r := range history {
		dailyTotals[r.Date] += r.TotalCost
		u := dailyUsage[r.Date]
		u.PromptTokens += r.Usage.PromptTokens
		u.ResponseTokens += r.Usage.ResponseTokens
		u.CachedTokens += r.Usage.CachedTokens
		u.ThinkingTokens += r.Usage.ThinkingTokens
		dailyUsage[r.Date] = u
	}

	var sb strings.Builder
	sb.WriteString("### AI Usage Cost Summary (by Date)\n\n")
	sb.WriteString("| Date | M | H | O | Total Cost (USD) |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- | :--- |\n")

	// Sort dates descending
	var dates []string
	for d := range dailyTotals {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))

	var grandTotal float64
	var totalM, totalH, totalO int64
	for _, d := range dates {
		cost := dailyTotals[d]
		u := dailyUsage[d]

		mTokens := u.PromptTokens - u.CachedTokens
		hTokens := u.CachedTokens
		oTokens := u.ResponseTokens + u.ThinkingTokens

		sb.WriteString(fmt.Sprintf("| %s | %d | %d | %d | $%.4f |\n", d, mTokens, hTokens, oTokens, cost))
		grandTotal += cost
		totalM += mTokens
		totalH += hTokens
		totalO += oTokens
	}
	sb.WriteString(fmt.Sprintf("| **Grand Total** | **%d** | **%d** | **%d** | **$%.4f** |\n", totalM, totalH, totalO, grandTotal))

	return sb.String(), nil
}

// GetPricing returns the hardcoded fallback pricing data.
func GetPricing(ctx context.Context, sm *security.SecurityManager, outputDir string) llm.PricingData {
	return config.DefaultPricing()
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
			Usage:     usage,
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
	}
	return stats, totalCost, detectedModel, scanner.Err()
}

// Accumulate adds metrics to usage statistics.
func Accumulate(stats *UsageStats, mt llm.Metrics, p llm.ModelPricing) {
	stats.PromptTokens += int64(mt.PromptTokens)
	stats.ResponseTokens += int64(mt.ResponseTokens)
	stats.CachedTokens += int64(mt.CachedTokens)
	stats.SearchQueries += int64(mt.SearchQueries)
	stats.ThinkingTokens += int64(mt.ThinkingTokens)
}

func (m *metricsManager) renderReport(pricing llm.PricingData, breakdown CostBreakdown) string {
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
	sb.WriteString(fmt.Sprintf("| Text Output | %d | $%.2f | $%.6f |\n", stats.ResponseTokens, p.Comp, breakdown.OutputCost))
	sb.WriteString(fmt.Sprintf("| Search Queries | %d | $%.3f/Q | $%.6f |\n", stats.SearchQueries, pricing.SearchQuery, breakdown.SearchCost))
	sb.WriteString("| **Total** | | | **$" + fmt.Sprintf("%.4f", breakdown.TotalCost) + "** |\n")

	return sb.String()
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
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
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

		usage, totalCost, detectedModel, err := ParseUsage(path, pricing, m.model)
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
				Usage:     usage,
			})
			seen[sessionID] = true
		} else if !os.IsNotExist(err) {
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
