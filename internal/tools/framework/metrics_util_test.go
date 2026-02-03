// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/pricing"
)

func TestParseUsage_Robustness(t *testing.T) {
	pricingData := pricing.PricingData{
		Models: map[string]pricing.ModelPricing{
			"gpt-4": {Miss: 10.0, Comp: 30.0}, // $10 per 1M input, $30 per 1M output
		},
	}

	const epsilon = 1e-9

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
			name: "Calculated cost (cost field missing)",
			content:        `{"model":"gpt-4", "prompt_tokens":1000000, "response_tokens":0}`,
			expectedCost:   10.0, // 1M tokens * $10/1M
			expectedPrompt: 1000000,
			expectedModel:  "gpt-4",
		},
		{
			name:           "Detected Model and Timestamp",
			content:        "{\"model\":\"gpt-4-custom\", \"timestamp\":\"2026-01-02T15:04:05Z\", \"cost\":0.1}\n",
			expectedCost:   0.1,
			expectedModel:  "gpt-4-custom",
			expectedTime:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.Create(filepath.Join(t.TempDir(), "test-usage.log"))
			if err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}

			if _, err := tmpFile.WriteString(tt.content); err != nil {
				t.Fatalf("failed to write to temp file: %v", err)
			}
			tmpFile.Close()

			stats, totalCost, detectedModel, timestamp, err := ParseUsage(tmpFile.Name(), pricingData, "gpt-4")
			if err != nil {
				t.Errorf("ParseUsage() unexpected error: %v", err)
			}

			if math.Abs(totalCost-tt.expectedCost) > epsilon {
				t.Errorf("totalCost = %v, want %v", totalCost, tt.expectedCost)
			}
			if tt.expectedPrompt > 0 && stats.PromptTokens != tt.expectedPrompt {
				t.Errorf("PromptTokens = %v, want %v", stats.PromptTokens, tt.expectedPrompt)
			}
			if tt.expectedResponse > 0 && stats.ResponseTokens != tt.expectedResponse {
				t.Errorf("ResponseTokens = %v, want %v", stats.ResponseTokens, tt.expectedResponse)
			}
			if tt.expectedModel != "" && detectedModel != tt.expectedModel {
				t.Errorf("detectedModel = %q, want %q", detectedModel, tt.expectedModel)
			}
			if tt.expectedTime && timestamp.IsZero() {
				t.Error("expected non-zero timestamp, got zero")
			}
			if !tt.expectedTime && !timestamp.IsZero() && tt.name != "Multi-line summation" && tt.name != "Calculated cost (cost field missing)" {
				// Special check for zero timestamp when not expected, but some cases might have it if not specified in JSON
			}
		})
	}
}

func TestParseUsage_InvalidPath(t *testing.T) {
	_, _, _, _, err := ParseUsage("non-existent-file.log", pricing.PricingData{}, "gpt-4")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestParseUsage_EmptyFile(t *testing.T) {
	tmpFile, err := os.Create(filepath.Join(t.TempDir(), "empty.log"))
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()

	stats, totalCost, detectedModel, timestamp, err := ParseUsage(tmpFile.Name(), pricing.PricingData{}, "gpt-4")
	if err != nil {
		t.Errorf("ParseUsage() unexpected error: %v", err)
	}

	if totalCost != 0 {
		t.Errorf("totalCost = %v, want 0", totalCost)
	}
	if !timestamp.IsZero() {
		t.Errorf("timestamp = %v, want zero", timestamp)
	}
	if detectedModel != "" {
		t.Errorf("detectedModel = %q, want empty", detectedModel)
	}
	if stats.PromptTokens != 0 {
		t.Errorf("stats.PromptTokens = %d, want 0", stats.PromptTokens)
	}
}
