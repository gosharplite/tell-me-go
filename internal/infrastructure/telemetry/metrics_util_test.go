// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

func TestParseUsage_Robustness(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			path := createTempLogFile(t, tt.content)
			stats, totalCost, model, timestamp, err := parseUsage(path, pricingData, "gpt-4")
			if err != nil {
				t.Errorf("parseUsage() unexpected error: %v", err)
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
	defer func() { _ = tmpFile.Close() }()

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
	t.Parallel()
	_, _, _, _, err := parseUsage("non-existent-file.log", domain_pricing.PricingData{}, "gpt-4")
	if err == nil {
		t.Error("expected error for non-existent file, got nil")
	}
}

func TestParseUsage_EmptyFile(t *testing.T) {
	t.Parallel()
	path := createTempLogFile(t, "")
	stats, totalCost, model, timestamp, err := parseUsage(path, domain_pricing.PricingData{}, "gpt-4")
	if err != nil {
		t.Errorf("parseUsage() unexpected error: %v", err)
	}

	assertParseResults(t, totalCost, 0, stats, 0, 0, model, "", timestamp, false)
}

func TestParseUsage_LargeLine(t *testing.T) {
	t.Parallel()
	// Create a log entry larger than the default 64KB limit (e.g., 70KB)

	largeField := strings.Repeat("a", 70*1024)
	content := `{"model":"gpt-4", "prompt_tokens":100, "thinking_tokens":100, "large_data":"` + largeField + `", "cost":0.5}` + "\n"
	path := createTempLogFile(t, content)

	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"gpt-4": {Miss: 10.0, Comp: 30.0},
		},
	}

	stats, totalCost, _, _, err := parseUsage(path, pricingData, "gpt-4")
	if err != nil {
		t.Fatalf("parseUsage failed on large line: %v", err)
	}

	if totalCost != 0.5 {
		t.Errorf("expected cost 0.5, got %v", totalCost)
	}
	if stats.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", stats.PromptTokens)
	}
}

func TestParseUsage_VeryLargeLine(t *testing.T) {
	t.Parallel()
	// Create a log entry larger than the previous 1MB limit (e.g., 2MB)

	largeField := strings.Repeat("a", 2*1024*1024)
	content := `{"model":"gpt-4", "prompt_tokens":100, "large_data":"` + largeField + `", "cost":0.5}` + "\n"
	path := createTempLogFile(t, content)

	pricingData := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"gpt-4": {Miss: 10.0, Comp: 30.0},
		},
	}

	stats, totalCost, _, _, err := parseUsage(path, pricingData, "gpt-4")
	if err != nil {
		t.Fatalf("parseUsage failed on very large line: %v", err)
	}

	if totalCost != 0.5 {
		t.Errorf("expected cost 0.5, got %v", totalCost)
	}
	if stats.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", stats.PromptTokens)
	}
}

func TestCalculate_UnifiedOutputRate(t *testing.T) {
	t.Parallel()
	pricing := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"ds-reasoner": {Miss: 0.27, Comp: 1.10},
			"claude":      {Miss: 3.00, Comp: 15.00},
		},
	}

	tests := []struct {
		name         string
		modelName    string
		stats        domain_pricing.UsageStats
		expectedCost float64
	}{
		{
			name:      "DeepSeek with Unified Thinking/Response",
			modelName: "ds-reasoner",
			stats:     domain_pricing.UsageStats{PromptTokens: 1000000, ResponseTokens: 1000000, ThinkingTokens: 1000000},
			// (1M * 0.27) + (2M * 1.10) = 0.27 + 2.20 = 2.47
			expectedCost: 2.47,
		},
		{
			name:      "Claude with Unified Thinking/Response",
			modelName: "claude",
			stats:     domain_pricing.UsageStats{PromptTokens: 1000000, ResponseTokens: 1000000, ThinkingTokens: 1000000},
			// (1M * 3.00) + (2M * 15.00) = 3.00 + 30.00 = 33.00
			expectedCost: 33.00,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := pricing.GetModelPricing(tt.modelName)
			calc := &domain_pricing.CostCalculator{Pricing: pricing, Model: p}
			breakdown := calc.Calculate(tt.stats)

			const epsilon = 1e-9
			if math.Abs(breakdown.TotalCost-tt.expectedCost) > epsilon {
				t.Errorf("Calculate() totalCost = %v, want %v", breakdown.TotalCost, tt.expectedCost)
			}
		})
	}
}

func TestGetPricing_LoadFromFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatalf("failed to create assets dir: %v", err)
	}
	outputDir := filepath.Join(tmpDir, "output")
	pricingFile := filepath.Join(assetsDir, "pricing.json")
	content := `{
		"updated_at": "2026-02-03T12:00:00Z",
		"models": {
			"test-model": {
				"hit": 1.0,
				"miss": 2.0,
				"comp": 3.0
			}
		}
	}`
	if err := os.WriteFile(pricingFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write pricing file: %v", err)
	}

	pd := GetPricing(context.Background(), nil, outputDir)
	if pd.UpdatedAt != "2026-02-03T12:00:00Z" {
		t.Errorf("expected UpdatedAt 2026-02-03T12:00:00Z, got %q", pd.UpdatedAt)
	}
	if pd.Models["test-model"].Hit != 1.0 {
		t.Errorf("expected test-model hit 1.0, got %v", pd.Models["test-model"].Hit)
	}
}

func TestGetPricing_FallbackOnMissingFile(t *testing.T) {
	t.Parallel()
	anotherDir := t.TempDir()
	pd := GetPricing(context.Background(), nil, filepath.Join(anotherDir, "output"))
	if pd.UpdatedAt != "2026-02-03T12:00:00Z" {
		t.Errorf("expected hardcoded fallback, got %q", pd.UpdatedAt)
	}
}

func TestGetPricing_FallbackOnInvalidJSON(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatalf("failed to create assets dir: %v", err)
	}
	outputDir := filepath.Join(tmpDir, "output")
	pricingFile := filepath.Join(assetsDir, "pricing.json")
	if err := os.WriteFile(pricingFile, []byte("invalid json"), 0644); err != nil {
		t.Fatalf("failed to write invalid pricing file: %v", err)
	}

	pd := GetPricing(context.Background(), nil, outputDir)
	if pd.UpdatedAt != "2026-02-03T12:00:00Z" {
		t.Errorf("expected hardcoded fallback on invalid JSON, got %q", pd.UpdatedAt)
	}
}

func TestGetPricing_FallbackOnUnreadableFile(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not prevent file reads on Windows")
	}

	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatalf("failed to create assets dir: %v", err)
	}
	outputDir := filepath.Join(tmpDir, "output")
	pricingFile := filepath.Join(assetsDir, "pricing.json")

	content := `{"updated_at": "2026-03-01T00:00:00Z", "models": {"m": {"hit": 1.0, "miss": 2.0, "comp": 3.0}}}`
	if err := os.WriteFile(pricingFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write pricing file: %v", err)
	}

	// Make the file unreadable so defaultLoader.loadFromDisk returns an error.
	if err := os.Chmod(pricingFile, 0000); err != nil {
		t.Fatalf("failed to chmod pricing file: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(pricingFile, 0644) })

	pd := GetPricing(context.Background(), nil, outputDir)

	// Should fall back to hardcoded default pricing.
	if pd.UpdatedAt != "2026-02-03T12:00:00Z" {
		t.Errorf("expected hardcoded fallback on unreadable file, got UpdatedAt=%q", pd.UpdatedAt)
	}
	// Also verify we got a real model entry, not a zero-value PricingData.
	if len(pd.Models) == 0 {
		t.Error("expected non-empty Models in fallback pricing data")
	}
}

func TestProcessLogLine_SkipsSummaryLine(t *testing.T) {
	t.Parallel()

	pricing := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"gpt-4": {Miss: 10.0, Comp: 30.0},
		},
	}

	state := &parseState{}

	// Construct a JSON line with is_summary=true and non-zero tokens.
	summaryJSON := `{"model":"gpt-4","prompt_tokens":100,"response_tokens":50,"is_summary":true}`

	err := processLogLine([]byte(summaryJSON), state, pricing, "gpt-4")
	if err != nil {
		t.Fatalf("processLogLine returned unexpected error: %v", err)
	}

	// The line should be skipped entirely — no accumulation.
	if state.stats.PromptTokens != 0 {
		t.Errorf("expected 0 prompt tokens (line skipped), got %d", state.stats.PromptTokens)
	}
	if state.totalCost != 0 {
		t.Errorf("expected 0 total cost (line skipped), got %f", state.totalCost)
	}
}

func TestGetPricing_CacheHit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(tmpDir, "output")
	pricingFile := filepath.Join(assetsDir, "pricing.json")

	content := `{
		"updated_at": "2026-03-01T00:00:00Z",
		"models": {
			"cached-model": {"hit": 1.0, "miss": 2.0, "comp": 3.0}
		}
	}`
	if err := os.WriteFile(pricingFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// First call: populate cache from disk.
	first := GetPricing(context.Background(), nil, outputDir)
	if first.Models["cached-model"].Hit != 1.0 {
		t.Fatalf("first call: expected hit=1.0, got %v", first.Models["cached-model"].Hit)
	}

	// Second call without touching the file: cache serves data because
	// os.Stat confirms the ModTime hasn't changed, skipping the slow path.
	second := GetPricing(context.Background(), nil, outputDir)
	if second.UpdatedAt != "2026-03-01T00:00:00Z" {
		t.Errorf("cache miss: expected UpdatedAt '2026-03-01T00:00:00Z' from cache, got %q", second.UpdatedAt)
	}
	if second.Models["cached-model"].Hit != 1.0 {
		t.Errorf("cache miss: expected hit=1.0 from cache, got %v", second.Models["cached-model"].Hit)
	}
}

func TestGetPricing_CacheInvalidation(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(tmpDir, "output")
	pricingFile := filepath.Join(assetsDir, "pricing.json")

	// Write v1 and force mtime to a known past timestamp.
	v1 := `{"updated_at": "2026-01-01T00:00:00Z", "models": {"m": {"hit": 1.0, "miss": 2.0, "comp": 3.0}}}`
	if err := os.WriteFile(pricingFile, []byte(v1), 0644); err != nil {
		t.Fatal(err)
	}
	t1 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(pricingFile, t1, t1); err != nil {
		t.Fatal(err)
	}

	first := GetPricing(context.Background(), nil, outputDir)
	if first.UpdatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("expected v1, got %q", first.UpdatedAt)
	}

	// Overwrite with v2 and force mtime to a different timestamp.
	v2 := `{"updated_at": "2026-06-15T00:00:00Z", "models": {"m": {"hit": 9.0, "miss": 8.0, "comp": 7.0}}}`
	if err := os.WriteFile(pricingFile, []byte(v2), 0644); err != nil {
		t.Fatal(err)
	}
	t2 := time.Date(2020, 1, 1, 0, 0, 1, 0, time.UTC) // 1 second after t1
	if err := os.Chtimes(pricingFile, t2, t2); err != nil {
		t.Fatal(err)
	}

	second := GetPricing(context.Background(), nil, outputDir)
	if second.UpdatedAt != "2026-06-15T00:00:00Z" {
		t.Errorf("cache not invalidated: expected UpdatedAt '2026-06-15T00:00:00Z', got %q", second.UpdatedAt)
	}
	if second.Models["m"].Hit != 9.0 {
		t.Errorf("cache not invalidated: expected hit=9.0, got %v", second.Models["m"].Hit)
	}
}

func TestGetPricing_Concurrency(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow concurrency test in short mode")
	}

	tmpDir := t.TempDir()
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(tmpDir, "output")
	pricingFile := filepath.Join(assetsDir, "pricing.json")

	content := `{"updated_at": "2026-03-15T00:00:00Z", "models": {"test": {"hit": 0.5, "miss": 1.0, "comp": 2.0}}}`
	if err := os.WriteFile(pricingFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				pd := GetPricing(ctx, nil, outputDir)
				// Verify we get either the file data or fallback (not zero-value)
				if pd.UpdatedAt == "" {
					t.Error("GetPricing returned empty UpdatedAt under concurrency")
				}
			}
		}()
	}

	wg.Wait()
}

// Sink variables for benchmarks — prevent compiler optimizations from eliding results.
var (
	benchSinkUsage     domain_pricing.UsageStats
	benchSinkCost      float64
	benchSinkModel     string
	benchSinkTimestamp time.Time
	benchSinkParseErr  error
)

// benchParseLine is a single realistic log line without a cost field,
// forcing calculateLineCost → CostCalculator.Calculate for every line.
const benchParseLine = `{"model":"claude-sonnet-4-20250514","prompt_tokens":1500,"response_tokens":800,"cached_tokens":200,"cache_write_tokens":100,"thinking_tokens":400,"search_queries":2,"timestamp":"2026-03-15T10:30:00Z"}` + "\n"

// benchParsePricing is the pricing fixture used by all parseUsage benchmarks.
var benchParsePricing = domain_pricing.PricingData{
	Models: map[string]domain_pricing.ModelPricing{
		"claude-sonnet-4-20250514": {Hit: 0.30, Miss: 3.00, Comp: 15.00, SearchQuery: 0.015},
	},
}

func BenchmarkParseUsage_Small(b *testing.B) {
	path := createBenchLogFile(b, benchParseLine)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchSinkUsage, benchSinkCost, benchSinkModel, benchSinkTimestamp, benchSinkParseErr =
			parseUsage(path, benchParsePricing, "")
	}

	_, _, _, _, _ = benchSinkUsage, benchSinkCost, benchSinkModel, benchSinkTimestamp, benchSinkParseErr
}

func BenchmarkParseUsage_Large(b *testing.B) {
	lines := make([]string, 1000)
	for i := range lines {
		lines[i] = benchParseLine[:len(benchParseLine)-1] // strip trailing \n
	}
	content := strings.Join(lines, "\n") + "\n"

	path := createBenchLogFile(b, content)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		benchSinkUsage, benchSinkCost, benchSinkModel, benchSinkTimestamp, benchSinkParseErr =
			parseUsage(path, benchParsePricing, "")
	}

	_, _, _, _, _ = benchSinkUsage, benchSinkCost, benchSinkModel, benchSinkTimestamp, benchSinkParseErr
}

// createBenchLogFile writes content to a temp file and returns its path.
func createBenchLogFile(b *testing.B, content string) string {
	b.Helper()
	tmpFile, err := os.Create(filepath.Join(b.TempDir(), "bench-usage.log"))
	if err != nil {
		b.Fatalf("failed to create temp file: %v", err)
	}
	defer func() { _ = tmpFile.Close() }()

	if _, err := tmpFile.WriteString(content); err != nil {
		b.Fatalf("failed to write to temp file: %v", err)
	}
	return tmpFile.Name()
}
