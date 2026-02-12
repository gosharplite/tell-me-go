// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

func TestParseUsage_Robustness(t *testing.T) {
	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"gpt-4": {Miss: 10.0, Comp: 30.0}, // $10 per 1M input, $30 per 1M output
		},
	}

	tests := []struct {
		name             string
		content          string
		expectedCost     float64
		expectedPrompt   int64
		expectedResponse int64
		expectedModel    string
		expectedTime     bool // if true, check if timestamp is non-zero
	}{
		{
			name:           "Leading Space",
			content:        "  {\"model\":\"gpt-4\", \"prompt_tokens\":100, \"cost\":0.5}\n",
			expectedCost:   0.5,
			expectedPrompt: 100,
			expectedModel:  "gpt-4",
		},
		{
			name:           "Non-JSON Prefix",
			content:        "LOG: {\"model\":\"gpt-4\", \"cost\":0.5}\n",
			expectedCost:   0.0,
			expectedPrompt: 0,
		},
		{
			name:           "Malformed JSON starting with {",
			content:        "{\"model\":\"gpt-4\", \"cost\":0.5, invalid\n",
			expectedCost:   0.0,
			expectedPrompt: 0,
		},
		{
			name:           "Empty lines",
			content:        "\n\n{\"model\":\"gpt-4\", \"cost\":0.1}\n\n",
			expectedCost:   0.1,
			expectedPrompt: 0,
			expectedModel:  "gpt-4",
		},
		{
			name:           "Valid JSON with trailing space",
			content:        "{\"model\":\"gpt-4\", \"cost\":0.2}   \n",
			expectedCost:   0.2,
			expectedPrompt: 0,
			expectedModel:  "gpt-4",
		},
		{
			name: "Multi-line summation",
			content: `{"model":"gpt-4", "prompt_tokens":100, "response_tokens":50, "cost":0.1}
{"model":"gpt-4", "prompt_tokens":200, "response_tokens":100, "cost":0.2}`,
			expectedCost:     0.3,
			expectedPrompt:   300,
			expectedResponse: 150,
			expectedModel:    "gpt-4",
		},
		{
			name:           "Calculated cost (cost field missing)",
			content:        `{"model":"gpt-4", "prompt_tokens":1000000, "response_tokens":0}`,
			expectedCost:   10.0, // 1M tokens * $10/1M
			expectedPrompt: 1000000,
			expectedModel:  "gpt-4",
		},
		{
			name:          "Detected Model and Timestamp",
			content:       "{\"model\":\"gpt-4-custom\", \"timestamp\":\"2026-01-02T15:04:05Z\", \"cost\":0.1}\n",
			expectedCost:  0.1,
			expectedModel: "gpt-4-custom",
			expectedTime:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := createTempLogFile(t, tt.content)
			stats, totalCost, model, timestamp, err := ParseUsage(path, pricingData, "gpt-4")
			if err != nil {
				t.Errorf("ParseUsage() unexpected error: %v", err)
			}

			assertParseResults(t, totalCost, tt.expectedCost, stats, tt.expectedPrompt, tt.expectedResponse, model, tt.expectedModel, timestamp, tt.expectedTime)
		})
	}
}

func createTempLogFile(t *testing.T, content string) string {
	t.Helper()
	tmpFile, err := os.Create(filepath.Join(t.TempDir(), "test-usage.log"))
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	return tmpFile.Name()
}

func assertParseResults(t *testing.T, cost, wantCost float64, stats domain_pricing.UsageStats, wantPrompt, wantResp int64, model, wantModel string, timestamp time.Time, wantTime bool) {
	t.Helper()
	const epsilon = 1e-9
	if math.Abs(cost-wantCost) > epsilon {
		t.Errorf("totalCost = %v, want %v", cost, wantCost)
	}
	if wantPrompt > 0 && stats.PromptTokens != wantPrompt {
		t.Errorf("PromptTokens = %v, want %v", stats.PromptTokens, wantPrompt)
	}
	if wantResp > 0 && stats.ResponseTokens != wantResp {
		t.Errorf("ResponseTokens = %v, want %v", stats.ResponseTokens, wantResp)
	}
	if wantModel != "" && model != wantModel {
		t.Errorf("detectedModel = %q, want %q", model, wantModel)
	}
	if wantTime && timestamp.IsZero() {
		t.Error("expected non-zero timestamp, got zero")
	}
}

func TestParseUsage_InvalidPath(t *testing.T) {
	_, _, _, _, err := ParseUsage("non-existent-file.log", domain_pricing.PricingData{}, "gpt-4")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestParseUsage_EmptyFile(t *testing.T) {
	path := createTempLogFile(t, "")
	stats, totalCost, model, timestamp, err := ParseUsage(path, domain_pricing.PricingData{}, "gpt-4")
	if err != nil {
		t.Errorf("ParseUsage() unexpected error: %v", err)
	}

	assertParseResults(t, totalCost, 0, stats, 0, 0, model, "", timestamp, false)
}

func TestParseUsage_LargeLine(t *testing.T) {
	// Create a log entry larger than the default 64KB limit (e.g., 70KB)
	largeField := strings.Repeat("a", 70*1024)
	content := `{"model":"gpt-4", "prompt_tokens":100, "thinking_tokens":100, "large_data":"` + largeField + `", "cost":0.5}` + "\n"
	path := createTempLogFile(t, content)

	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"gpt-4": {Miss: 10.0, Comp: 30.0},
		},
	}

	stats, totalCost, _, _, err := ParseUsage(path, pricingData, "gpt-4")
	if err != nil {
		t.Fatalf("ParseUsage failed on large line: %v", err)
	}

	if totalCost != 0.5 {
		t.Errorf("expected cost 0.5, got %v", totalCost)
	}
	if stats.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", stats.PromptTokens)
	}
}

func TestParseUsage_VeryLargeLine(t *testing.T) {
	// Create a log entry larger than the previous 1MB limit (e.g., 2MB)
	largeField := strings.Repeat("a", 2*1024*1024)
	content := `{"model":"gpt-4", "prompt_tokens":100, "large_data":"` + largeField + `", "cost":0.5}` + "\n"
	path := createTempLogFile(t, content)

	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"gpt-4": {Miss: 10.0, Comp: 30.0},
		},
	}

	stats, totalCost, _, _, err := ParseUsage(path, pricingData, "gpt-4")
	if err != nil {
		t.Fatalf("ParseUsage failed on very large line: %v", err)
	}

	if totalCost != 0.5 {
		t.Errorf("expected cost 0.5, got %v", totalCost)
	}
	if stats.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", stats.PromptTokens)
	}
}
