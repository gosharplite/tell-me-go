// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bufio"
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

	"google.golang.org/genai"
)

var (
	metricsMu sync.Mutex
	pricingMu sync.Mutex
)

const pricingURL = "https://raw.githubusercontent.com/gosharplite/tell-me-go/main/assets/pricing.json"

type ModelPricing struct {
	Hit             float64 `json:"hit"`
	Miss            float64 `json:"miss"`
	Comp            float64 `json:"comp"`
	TieredThreshold int64   `json:"tiered_threshold"`
	TieredMiss      float64 `json:"tiered_miss"`
	TieredComp      float64 `json:"tiered_comp"`
	ThinkingBudget  int     `json:"thinking_budget,omitempty"`
}

type PricingData struct {
	UpdatedAt       string                  `json:"updated_at"`
	Models          map[string]ModelPricing `json:"models"`
	ThinkingBudgets map[string]int          `json:"thinking_budgets,omitempty"`
	SearchQuery     float64                 `json:"search_query"`
}

// SessionCostRecord represents a single session's financial footprint.
type SessionCostRecord struct {
	Date      string  `json:"date"`
	Session   string  `json:"session"`
	Model     string  `json:"model"`
	TotalCost float64 `json:"total_cost"`
}

// RegisterMetricsTools adds tools for usage and cost analysis.
func RegisterMetricsTools(r *Registry, logFile string, model string, mode string) {
	r.Register(&genai.FunctionDeclaration{
		Name:        "estimate_cost",
		Description: "Calculates the estimated USD cost of the current session.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
		},
	}, func(args map[string]interface{}) (string, error) {
		return EstimateCost(logFile, model, mode, true, "") // Records to ledger with default ID
	})

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_cost_summary",
		Description: "Returns a summary of total AI costs grouped by date from the local history ledger.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
		},
	}, func(args map[string]interface{}) (string, error) {
		// Silent update: Calculate and record the current session's latest cost before summary.
		_, _ = EstimateCost(logFile, model, mode, true, "")
		return getCostSummary(filepath.Dir(logFile))
	})
}

// RecordSessionCost calculates and saves the session cost to the global ledger.
// It is intended for use in the application lifecycle (archiving/shutdown).
// sessionID allows providing a unique name (e.g. timestamped) to avoid overwriting entries in the ledger.
func RecordSessionCost(logFile, model, mode, sessionID string) error {
	_, err := EstimateCost(logFile, model, mode, true, sessionID)
	return err
}

// recordCost saves the current cost to a persistent local ledger with file locking to prevent corruption.
func recordCost(outputDir string, mode string, record SessionCostRecord) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	historyPath := filepath.Join(outputDir, "global_costs.json")
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
	configPath := filepath.Join(outputDir, mode+"_config.json")
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
	if bytes, err := json.MarshalIndent(history, "", "  "); err == nil {
		_ = AtomicWrite(historyPath, bytes, 0644)
	}
}

func getCostSummary(outputDir string) (string, error) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	historyPath := filepath.Join(outputDir, "global_costs.json")
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
func GetPricing(outputDir string) PricingData {
	pricingMu.Lock()
	defer pricingMu.Unlock()
	cachePath := filepath.Join(outputDir, "global_prices.json")
	var data PricingData
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
		resp, err := client.Get(pricingURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				// Save to cache atomically
				if bytes, err := json.MarshalIndent(data, "", "  "); err == nil {
					_ = AtomicWrite(cachePath, bytes, 0644)
				}
				useCache = true
			}
		}
	}

	// 3. Hardcoded Fallback (Updated for Gemini 3 Preview)
	if !useCache {
		data = PricingData{
			UpdatedAt: "Hardcoded Fallback",
			Models: map[string]ModelPricing{
				"flash": {
					Hit:             0.025,
					Miss:            0.025,
					Comp:            0.30,
					TieredThreshold: 128000,
					TieredMiss:      0.075,
					TieredComp:      0.30,
				},
				"pro": {
					Hit:             0.3125,
					Miss:            0.3125,
					Comp:            3.75,
					TieredThreshold: 128000,
					TieredMiss:      1.25,
					TieredComp:      7.50,
				},
				"default": {
					Hit:             0.3125,
					Miss:            0.3125,
					Comp:            3.75,
					TieredThreshold: 128000,
					TieredMiss:      1.25,
					TieredComp:      7.50,
				},
			},
			ThinkingBudgets: map[string]int{
				"gemini-2.5-flash":       24576,
				"gemini-3-flash-preview": 32768,
			},
			SearchQuery: 0.014,
		}
	}

	return data
}

func EstimateCost(logFile string, model string, mode string, shouldRecord bool, sessionID string) (string, error) {
	if err := IsPathSafe(logFile); err != nil {
		return "", err
	}

	outputDir := filepath.Dir(logFile)
	pricing := GetPricing(outputDir)

	f, err := os.Open(logFile)
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
		m, _ := strconv.ParseInt(parts[4], 10, 64)
		c, _ := strconv.ParseInt(parts[6], 10, 64)
		s, _ := strconv.ParseInt(parts[12], 10, 64)
		th, _ := strconv.ParseInt(parts[14], 10, 64)

		totalH += h
		totalM += m
		totalC += c
		totalS += s
		totalTh += th

		// Pricing Selection
		var p ModelPricing
		if strings.Contains(model, "flash") {
			p = pricing.Models["flash"]
		} else if strings.Contains(model, "pro") {
			p = pricing.Models["pro"]
		} else {
			p = pricing.Models["default"]
		}

		rh, rm, rc := p.Hit, p.Miss, p.Comp
		if p.TieredThreshold > 0 && (h+m) > p.TieredThreshold {
			rm, rc = p.TieredMiss, p.TieredComp
		}

		costH += (float64(h) * rh / 1e6)
		costM += (float64(m) * rm / 1e6)
		costC += (float64(c) * rc / 1e6)
		costTh += (float64(th) * rc / 1e6)
		costS += float64(s) * pricing.SearchQuery
	}

	totalCost := costH + costM + costC + costTh + costS

	// Persistence: Record to local ledger
	if shouldRecord {
		if sessionID == "" {
			sessionID = filepath.Base(logFile)
		}
		recordCost(outputDir, mode, SessionCostRecord{
			Date:      time.Now().Format("2006-01-02"),
			Session:   sessionID,
			Model:     model,
			TotalCost: totalCost,
		})
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Estimated Cost for Session (Model: %s):\n", model))
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
		var p ModelPricing
		if strings.Contains(model, "flash") {
			p = pricing.Models["flash"]
		} else if strings.Contains(model, "pro") {
			p = pricing.Models["pro"]
		} else {
			p = pricing.Models["default"]
		}

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
