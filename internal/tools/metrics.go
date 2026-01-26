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
	"strconv"
	"strings"
	"time"

	"google.golang.org/genai"
)

const pricingURL = "https://raw.githubusercontent.com/gosharplite/tell-me-go/dev/assets/pricing.json"

type ModelPricing struct {
	Hit             float64 `json:"hit"`
	Miss            float64 `json:"miss"`
	Comp            float64 `json:"comp"`
	TieredThreshold int64   `json:"tiered_threshold"`
	TieredMiss      float64 `json:"tiered_miss"`
	TieredComp      float64 `json:"tiered_comp"`
}

type PricingData struct {
	UpdatedAt   string                  `json:"updated_at"`
	Models      map[string]ModelPricing `json:"models"`
	SearchQuery float64                 `json:"search_query"`
}

// RegisterMetricsTools adds tools for usage and cost analysis.
func RegisterMetricsTools(r *Registry, logFile string, model string) {
	r.Register(&genai.FunctionDeclaration{
		Name:        "estimate_cost",
		Description: "Calculates the estimated USD cost of the current session based on token usage and grounding queries recorded in the log file.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
		},
	}, func(args map[string]interface{}) (string, error) {
		return estimateCost(logFile, model)
	})
}

// getPricing handles the tiered fetching of pricing data: Local Cache -> Remote -> Hardcoded Fallback.
func getPricing(outputDir string) PricingData {
	cachePath := filepath.Join(outputDir, "prices.json")
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
		client := http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(pricingURL)
		if err == nil && resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
				// Save to cache
				if bytes, err := json.MarshalIndent(data, "", "  "); err == nil {
					_ = os.WriteFile(cachePath, bytes, 0644)
				}
				useCache = true
			}
		}
	}

	// 3. Hardcoded Fallback
	if !useCache {
		data = PricingData{
			UpdatedAt: "Hardcoded Fallback",
			Models: map[string]ModelPricing{
				"flash": {Hit: 0.05, Miss: 0.50, Comp: 3.00},
				"pro":   {Hit: 0.20, Miss: 2.00, Comp: 12.00, TieredThreshold: 200000, TieredMiss: 4.00, TieredComp: 18.00},
				"default": {Hit: 0.3125, Miss: 1.25, Comp: 3.75, TieredThreshold: 128000, TieredMiss: 2.50, TieredComp: 7.50},
			},
			SearchQuery: 0.014,
		}
	}

	return data
}

func estimateCost(logFile string, model string) (string, error) {
	if err := IsPathSafe(logFile); err != nil {
		return "", err
	}

	pricing := getPricing(filepath.Dir(logFile))

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

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Estimated Cost for Session (Model: %s):\n", model))
	sb.WriteString(fmt.Sprintf("Pricing Data As Of: %s\n\n", pricing.UpdatedAt))
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
