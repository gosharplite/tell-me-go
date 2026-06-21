// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

func (m *metricsManager) recordCost(ctx context.Context, outputDir string, mode string, record sessionCostRecord) {
	globalDir := filepath.Dir(outputDir)
	historyPath := filepath.Join(globalDir, "global_costs.json")

	// Phase 1: Check ledger store availability under metricsMu only.
	// Trigger async recovery if the ledger store exists (no file I/O here —
	// triggerLedgerRecovery does a sync.Map check + goroutine spawn).
	m.metricsMu.Lock()
	m.checkLedgerAndTriggerRecovery(ctx, historyPath, globalDir)
	m.metricsMu.Unlock()

	// Phase 2: Load configuration outside all locks.
	retentionDays := m.loadRetentionDays(ctx)

	// Phase 3: Read-modify-write under ledgerMu + file lock.
	// The file lock serialises writers; re-reading after lock acquisition
	// prevents TOCTOU lost-update races between concurrent recordCost calls.
	ledgerMu.Lock()
	defer ledgerMu.Unlock()

	lockPath := historyPath + ".lock"
	lock, err := acquireLedgerLock(lockPath)
	if err != nil {
		slog.Warn("failed to acquire ledger lock (contention)",
			slog.String("path", lockPath),
			slog.Any("error", err))
		return
	}
	defer func() {
		_ = lock.Close()
		_ = os.Remove(lockPath)
	}()

	// Re-read the latest state under the write lock to guarantee
	// a consistent read-modify-write cycle.
	history, _, err := loadHistoryFromDisk(historyPath)
	if err != nil {
		slog.Warn("failed to read ledger under lock",
			slog.String("path", historyPath),
			slog.Any("error", err))
		return
	}

	history = upsertRecord(history, record)
	history = m.applyRetentionPolicy(history, retentionDays)

	bytes, err := json.Marshal(history)
	if err != nil {
		slog.Warn("failed to marshal ledger",
			slog.String("path", historyPath),
			slog.Any("error", err))
		return
	}
	if err := persistence.AtomicWrite(ctx, &persistence.OSFileSystem{}, historyPath, bytes, 0644); err != nil {
		slog.Warn("failed to write ledger",
			slog.String("path", historyPath),
			slog.Any("error", err))
	}
}

// loadHistoryFromDisk reads and parses the ledger file from disk.
// It returns the parsed records, whether the file existed on disk,
// and any non-fatal error (corruption errors are handled internally
// by backing up the corrupt file and returning an empty slice).
//
// This function performs NO mutex-guarded operations and does not
// access m.ledger — it is safe to call without holding metricsMu.
func loadHistoryFromDisk(historyPath string) ([]sessionCostRecord, bool, error) {
	content, err := os.ReadFile(historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []sessionCostRecord{}, false, nil
		}
		return []sessionCostRecord{}, false, err
	}

	var history []sessionCostRecord
	if err := json.Unmarshal(content, &history); err != nil {
		slog.Warn("failed to parse ledger, backing up and starting fresh",
			slog.String("path", historyPath),
			slog.Any("error", err))
		if err := os.Rename(historyPath, historyPath+".bak"); err != nil {
			slog.Warn("failed to backup corrupted ledger",
				slog.Any("error", err))
		}
		return []sessionCostRecord{}, true, nil
	}

	return history, true, nil
}

func (m *metricsManager) loadHistory(ctx context.Context, historyPath, globalDir string) []sessionCostRecord {
	history, fileExisted, _ := loadHistoryFromDisk(historyPath)
	if !fileExisted {
		m.checkLedgerAndTriggerRecovery(ctx, historyPath, globalDir)
	}
	return history
}

// checkLedgerAndTriggerRecovery checks whether a ledger store is available
// and, if the ledger file is missing, starts an asynchronous recovery.
// Must be called under metricsMu.
func (m *metricsManager) checkLedgerAndTriggerRecovery(ctx context.Context, historyPath, globalDir string) {
	if m.ledger != nil {
		m.triggerLedgerRecovery(ctx, historyPath, globalDir)
	}
}

func (m *metricsManager) triggerLedgerRecovery(ctx context.Context, historyPath, globalDir string) {
	if _, recovering := recoveryInProgress.Load(historyPath); !recovering {
		// Use a background context for recovery so it's not aborted if the request context is cancelled.
		bgCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ledgerRecoveryTimeout)
		go func() {
			defer cancel()
			m.ledger.recoverLedger(bgCtx, globalDir)
		}()
	}
}

func (m *metricsManager) updateLedgerHistory(ctx context.Context, historyPath, globalDir, outputDir string, record sessionCostRecord) {
	history := m.loadHistory(ctx, historyPath, globalDir)
	history = upsertRecord(history, record)
	history = m.applyRetentionPolicy(history, m.loadRetentionDays(ctx))

	// Write back atomically
	bytes, err := json.Marshal(history)
	if err != nil {
		slog.Warn("failed to marshal ledger",
			slog.String("path", historyPath),
			slog.Any("error", err))
		return
	}
	if err := persistence.AtomicWrite(ctx, &persistence.OSFileSystem{}, historyPath, bytes, 0644); err != nil {
		slog.Warn("failed to write ledger",
			slog.String("path", historyPath),
			slog.Any("error", err))
	}
}

func upsertRecord(history []sessionCostRecord, record sessionCostRecord) []sessionCostRecord {
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
	return history
}

func (m *metricsManager) getModelPricing(modelName string, pd domain_pricing.PricingData) domain_pricing.ModelPricing {
	return pd.GetModelPricing(modelName)
}

// recordCostIfNeeded persists the cost breakdown to the local ledger when
// shouldRecord is true. It generates a session ID and timestamp if needed.
func (m *metricsManager) recordCostIfNeeded(ctx context.Context, shouldRecord bool, sessionID, detectedModel string, timestamp time.Time, outputDir string, breakdown domain_pricing.CostBreakdown, usage domain_pricing.UsageStats) {
	if !shouldRecord {
		return
	}
	if sessionID == "" {
		sessionID = generateSessionID(m.mode, m.logFile)
	}
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	loc := time.FixedZone("UTC-8", -8*3600)
	m.recordCost(ctx, outputDir, m.mode, sessionCostRecord{
		Date:      timestamp.In(loc).Format("2006-01-02"),
		Timestamp: timestamp,
		Session:   sessionID,
		Model:     detectedModel,
		TotalCost: breakdown.TotalCost,
		Usage:     usage,
	})
}

// applyPricingOverrides applies configured pricing overrides to the pricing data.
func (m *metricsManager) applyPricingOverrides(pd domain_pricing.PricingData) domain_pricing.PricingData {
	for k, v := range m.pricingOverrides {
		pd.Models[k] = v
	}
	return pd
}

// resolveModel returns the detected model, falling back to the configured model
// when detection yields an empty string.
func (m *metricsManager) resolveModel(detectedModel string) string {
	if detectedModel == "" {
		return m.model
	}
	return detectedModel
}

func (m *metricsManager) EstimateCost(ctx context.Context, shouldRecord bool, sessionID string) (string, error) {
	resolvedLog, err := m.sm.IsPathSafe(m.logFile)
	if err != nil {
		return "", err
	}

	outputDir := filepath.Dir(resolvedLog)
	pd := GetPricing(ctx, m.sm, outputDir)
	pd = m.applyPricingOverrides(pd)

	// 1. Parse usage from log
	usage, totalCost, detectedModel, timestamp, err := parseUsage(resolvedLog, pd, m.model)
	if err != nil {
		if os.IsNotExist(err) {
			return "Error: Log file not found. Ensure you have made at least one request.", nil
		}
		return "", fmt.Errorf("failed to parse usage log: %w", err)
	}

	detectedModel = m.resolveModel(detectedModel)

	// 2. Delegate financial math to Calculator
	p := GetModelPricing(detectedModel, pd)
	calc := &domain_pricing.CostCalculator{Pricing: pd, Model: p}
	breakdown := calc.Calculate(usage)
	breakdown.TotalCost = totalCost // Use the per-turn accurate total cost

	m.recordCostIfNeeded(ctx, shouldRecord, sessionID, detectedModel, timestamp, outputDir, breakdown, usage)

	// 4. Render report
	return m.renderReport(pd, breakdown), nil
}

func (m *metricsManager) renderReport(pricing domain_pricing.PricingData, breakdown domain_pricing.CostBreakdown) string {
	p := m.getModelPricing(m.model, pricing)
	stats := breakdown.Stats

	var sb strings.Builder
	fmt.Fprintf(&sb, "Estimated Cost for Session (Model: %s):\n", m.model)
	fmt.Fprintf(&sb, "Pricing Data As Of: %s\n", pricing.UpdatedAt)
	sb.WriteString("\n")

	sb.WriteString("| SKU | Count | Rate (USD/1M) | Cost (USD) |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- |\n")

	fmt.Fprintf(&sb, "| Text Input | %d | $%.2f | $%.6f |\n", stats.PromptTokens-stats.CachedTokens, p.Miss, breakdown.InputCost)
	fmt.Fprintf(&sb, "| Input Caching | %d | $%.2f | $%.6f |\n", stats.CachedTokens, p.Hit, breakdown.CacheCost)
	if stats.CacheWriteTokens > 0 {
		fmt.Fprintf(&sb, "| Cache Write | %d | $%.2f | $%.6f |\n", stats.CacheWriteTokens, p.Miss*1.25, breakdown.CacheWriteCost)
	}

	fmt.Fprintf(&sb, "| Text Output | %d | $%.2f | $%.6f |\n", stats.ResponseTokens, p.Comp, (float64(stats.ResponseTokens) * p.Comp / 1e6))
	if stats.ThinkingTokens > 0 {
		fmt.Fprintf(&sb, "| Thinking Tokens | %d | $%.2f | $%.6f |\n", stats.ThinkingTokens, p.Comp, (float64(stats.ThinkingTokens) * p.Comp / 1e6))
	}
	fmt.Fprintf(&sb, "| Search Queries | %d | $%.3f/Q | $%.6f |\n", stats.SearchQueries, p.SearchQuery, breakdown.SearchCost)
	sb.WriteString("| **Total** | | | **$" + fmt.Sprintf("%.4f", breakdown.TotalCost) + "** |\n")

	return sb.String()
}

func (m *metricsManager) loadRetentionDays(ctx context.Context) int {
	retentionDays := 30
	if m.kvStore != nil {
		if val, err := m.kvStore.Get(ctx, "backup_retention_days"); err == nil && val != "" {
			if parsed, err := strconv.Atoi(val); err == nil {
				retentionDays = parsed
			}
		}
	}
	return retentionDays
}

func (m *metricsManager) applyRetentionPolicy(history []sessionCostRecord, retentionDays int) []sessionCostRecord {
	if retentionDays <= 0 {
		return history
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Format("2006-01-02")
	filtered := make([]sessionCostRecord, 0, len(history))
	for _, r := range history {
		if r.Date >= cutoff {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
