// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

func (m *metricsManager) getCostSummary(ctx context.Context, args costSummaryArgs) (string, error) {
	outputDir := filepath.Dir(m.logFile)
	globalDir := filepath.Dir(outputDir)
	historyPath := filepath.Join(globalDir, "global_costs.json")

	history, status, err := func() ([]sessionCostRecord, string, error) {
		m.metricsMu.Lock()
		defer m.metricsMu.Unlock()
		ledgerMu.Lock()
		defer ledgerMu.Unlock()
		return m.ensureLedgerReady(ctx, historyPath, globalDir)
	}()
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

func (m *metricsManager) ensureLedgerReady(ctx context.Context, historyPath, globalDir string) ([]sessionCostRecord, string, error) {
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

	var history []sessionCostRecord
	if err := json.Unmarshal(content, &history); err != nil {
		return nil, "Error parsing cost history. The file may be corrupted.", err
	}

	return history, "", nil
}

func (m *metricsManager) getRecordTimestamp(r sessionCostRecord) time.Time {
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

func (m *metricsManager) resolveGroupingKey(r sessionCostRecord, loc *time.Location, format, groupBy string) string {
	ts := m.getRecordTimestamp(r)
	if ts.IsZero() {
		return ""
	}

	switch groupBy {
	case "model":
		effectiveKey := r.Model
		if effectiveKey == "" {
			effectiveKey = "unknown"
		}
		return effectiveKey
	case "date,model":
		datePart := ts.In(loc).Format(format)
		modelPart := r.Model
		if modelPart == "" {
			modelPart = "unknown"
		}
		return fmt.Sprintf("%s | %s", datePart, modelPart)
	default:
		return ts.In(loc).Format(format)
	}
}

func (m *metricsManager) accumulateRecord(totals map[string]float64, usage map[string]domain_pricing.UsageStats, key string, r sessionCostRecord) {
	totals[key] += r.TotalCost
	u := usage[key]
	u.PromptTokens += r.Usage.PromptTokens
	u.ResponseTokens += r.Usage.ResponseTokens
	u.CachedTokens += r.Usage.CachedTokens
	u.ThinkingTokens += r.Usage.ThinkingTokens
	u.CacheWriteTokens += r.Usage.CacheWriteTokens
	u.SearchQueries += r.Usage.SearchQueries
	usage[key] = u
}

func (m *metricsManager) aggregateHistory(history []sessionCostRecord, start, end time.Time, loc *time.Location, format string, groupBy string) (map[string]float64, map[string]domain_pricing.UsageStats) {
	intervalTotals := make(map[string]float64)
	intervalUsage := make(map[string]domain_pricing.UsageStats)

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

		key := m.resolveGroupingKey(r, loc, format, groupBy)
		if key != "" {
			m.accumulateRecord(intervalTotals, intervalUsage, key, r)
		}
	}
	return intervalTotals, intervalUsage
}

func (m *metricsManager) aggregateCosts(history []sessionCostRecord, args costSummaryArgs) (map[string]float64, map[string]domain_pricing.UsageStats, []string, *time.Location, error) {
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

	intervalTotals, intervalUsage := m.aggregateHistory(history, start, end, location, format, args.GroupBy)

	// Sort keys descending
	keys := make([]string, 0, len(intervalTotals))
	for k := range intervalTotals {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))

	return intervalTotals, intervalUsage, keys, location, nil
}

func (m *metricsManager) formatSummaryTable(args costSummaryArgs, intervalTotals map[string]float64, intervalUsage map[string]domain_pricing.UsageStats, keys []string, location *time.Location) string {
	title := "AI Usage Cost Summary (by Date)"
	headerName := "Date"
	if args.GroupBy == "model" {
		title = "AI Usage Cost Summary (by Model)"
		headerName = "Model"
	} else if args.GroupBy == "date,model" {
		title = "AI Usage Cost Summary (by Date and Model)"
		headerName = "Date | Model"
	} else if args.Interval == "hour" {
		title = "AI Usage Cost Summary (by Hour)"
		headerName = "Date/Hour"
	}
	if args.Billing {
		title += " - Google Billing Cycle (UTC-8)"
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "### %s\n\n", title)
	_, _ = fmt.Fprintf(&sb, "| %s | Miss | Hit | Other | Eff %% | Total Cost (USD) |\n", headerName)
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

		_, _ = fmt.Fprintf(&sb, "| %s | %d | %d | %d | %.1f%% | $%.4f |\n", k, mTokens, hTokens, oTokens, eff, cost)
		grandTotal += cost
		totalM += mTokens
		totalH += hTokens
		totalO += oTokens
	}

	totalEff := 0.0
	if total := totalM + totalH; total > 0 {
		totalEff = float64(totalH) / float64(total) * 100
	}
	_, _ = fmt.Fprintf(&sb, "| **Grand Total** | **%d** | **%d** | **%d** | **%.1f%%** | **$%.4f** |\n", totalM, totalH, totalO, totalEff, grandTotal)

	return sb.String()
}
