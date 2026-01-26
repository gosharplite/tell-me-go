// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"google.golang.org/genai"
)

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

func estimateCost(logFile string, model string) (string, error) {
	if err := IsPathSafe(logFile); err != nil {
		return "", err
	}
	f, err := os.Open(logFile)
	if err != nil {
		return "Error: Log file not found. Ensure you have made at least one request.", nil
	}
	defer f.Close()

	var totalH, totalM, totalC, totalS, totalTh int64
	var totalCost float64

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		// [Time] H: 0 M: 45201 C: 217 T: 46102 N: 45418(98%) S: 1 Th: 1540 [13.5s]
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

		// Pricing calculation
		var rh, rm, rc float64
		if strings.Contains(model, "pro") {
			rh, rm, rc = 0.20, 2.00, 12.00
			if (h + m) > 200000 {
				rm, rc = 4.00, 18.00
			}
		} else if strings.Contains(model, "flash") {
			rh, rm, rc = 0.05, 0.50, 3.00
		} else {
			// Fallback to 1.5 Pro standard rates
			rh, rm, rc = 0.3125, 1.25, 3.75
			if (h + m) > 128000 {
				rm, rc = 2.50, 7.50
			}
		}

		totalCost += (float64(h) * rh / 1e6) + (float64(m) * rm / 1e6) + (float64(c+th) * rc / 1e6)
		// Grounding cost (Search)
		totalCost += float64(s) * 0.014
	}

	return fmt.Sprintf("Estimated Cost for Session:\n- Model: %s\n- Tokens: Hit: %d, Miss: %d, Comp: %d, Thinking: %d\n- Search Queries: %d\n- Total Cost: $%.4f",
		model, totalH, totalM, totalC, totalTh, totalS, totalCost), nil
}
