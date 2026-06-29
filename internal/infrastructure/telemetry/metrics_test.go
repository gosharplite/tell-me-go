// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_telemetry "github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/require"
)

func TestCostCalculator_Calculate(t *testing.T) {
	t.Parallel()
	pricingData := domain_pricing.PricingData{}
	modelPricing := domain_pricing.ModelPricing{
		Hit:         0.1,
		Miss:        1.0,
		Comp:        2.0,
		SearchQuery: 0.01,
	}

	calc := &domain_pricing.CostCalculator{
		Pricing: pricingData,
		Model:   modelPricing,
	}

	tests := []struct {
		name     string
		stats    domain_pricing.UsageStats
		wantCost float64
	}{
		{
			name: "Standard usage",
			stats: domain_pricing.UsageStats{
				CachedTokens:   1000000, // $0.1
				PromptTokens:   2000000, // 1000000 miss * $1.0 = $1.0
				ResponseTokens: 1000000, // $2.0
				SearchQueries:  1,       // $0.01
			},
			wantCost: 3.11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := calc.Calculate(tt.stats)
			if got.TotalCost != tt.wantCost {
				t.Errorf("Calculate() TotalCost = %v, want %v", got.TotalCost, tt.wantCost)
			}
		})
	}
}

func TestAccumulate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		mt           llm.Metrics
		wantPrompt   int64
		wantResponse int64
		wantCached   int64
		wantSearch   int64
		wantThinking int64
	}{
		{
			name: "Basic",
			mt: llm.Metrics{
				CachedTokens:   100,
				PromptTokens:   1000,
				ResponseTokens: 200,
				SearchQueries:  1,
				ThinkingTokens: 50,
			},
			wantPrompt:   1000,
			wantResponse: 200,
			wantCached:   100,
			wantSearch:   1,
			wantThinking: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stats := &domain_pricing.UsageStats{}
			accumulate(stats, tt.mt)

			if stats.PromptTokens != tt.wantPrompt {
				t.Errorf("PromptTokens = %v, want %v", stats.PromptTokens, tt.wantPrompt)
			}
			if stats.ResponseTokens != tt.wantResponse {
				t.Errorf("ResponseTokens = %v, want %v", stats.ResponseTokens, tt.wantResponse)
			}
			if stats.CachedTokens != tt.wantCached {
				t.Errorf("CachedTokens = %v, want %v", stats.CachedTokens, tt.wantCached)
			}
			if stats.SearchQueries != tt.wantSearch {
				t.Errorf("SearchQueries = %v, want %v", stats.SearchQueries, tt.wantSearch)
			}
			if stats.ThinkingTokens != tt.wantThinking {
				t.Errorf("ThinkingTokens = %v, want %v", stats.ThinkingTokens, tt.wantThinking)
			}
		})
	}
}

// =============================================================================
// logTrace error-path tests — covering silent warning gaps
// =============================================================================

// TestLogTrace_ErrorPaths exercises the error-handling branches in logTrace
// (metrics.go:233-267), including early returns and I/O failure warnings.
//
// UNREACHABLE: json.Marshal(trace) — TurnTrace contains only standard
// JSON-serializable types (string, time.Time, time.Duration as int64,
// []ToolExecutionTrace, bool). json.Marshal cannot fail on these fields.
//
// SUBTLE: f.Close() error — triggering an os.File.Close error on a regular
// file requires OS-level manipulation (e.g., double-close or disk failure);
// this path is exercised at the integration level, not in unit tests.
func TestLogTrace_ErrorPaths(t *testing.T) {
	tl := NewTraceLogger(nil)

	t.Run("nil trace", func(t *testing.T) {
		// Should return immediately without panic.
		tl.logTrace(context.Background(), "/some/file", nil)
	})

	t.Run("empty trace file", func(t *testing.T) {
		trace := &domain_telemetry.TurnTrace{}
		// Should return immediately without panic.
		tl.logTrace(context.Background(), "", trace)
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		trace := &domain_telemetry.TurnTrace{}
		// Should detect ctx.Done() and return without writing.
		tl.logTrace(ctx, "/some/file", trace)
	})

	t.Run("open file error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod not effective on Windows")
		}
		tmpDir := t.TempDir()
		require.NoError(t, os.Chmod(tmpDir, 0555))
		t.Cleanup(func() { _ = os.Chmod(tmpDir, 0755) })

		trace := &domain_telemetry.TurnTrace{}
		// logTrace calls os.OpenFile directly (no MkdirAll) — writing to a
		// subdirectory within a read-only parent triggers the "Failed to open
		// trace file" warning branch.
		tl.logTrace(context.Background(), filepath.Join(tmpDir, "sub", "trace.log"), trace)
	})
}

// =============================================================================
// appendSummaryToLog error-path tests — covering silent data-loss gaps
// =============================================================================

// TestAppendSummaryToLog_ErrorPaths exercises the error-handling branches in
// appendSummaryToLog (metrics.go:189-227), including the zero-usage early
// return and I/O permission failures.
//
// UNREACHABLE: json.Marshal(summary) — the marshaled value is llm.Metrics,
// whose fields are all standard JSON-serializable types (string, int32, int,
// float64, bool). json.Marshal cannot fail on these fields.
//
// UNREACHABLE: fAppend.WriteString — when OpenFile succeeds with O_WRONLY
// and O_APPEND, the only way WriteString fails on a regular file is via disk
// full or hardware failure; these are integration-level concerns.
func TestAppendSummaryToLog_ErrorPaths(t *testing.T) {
	t.Run("zero usage — early return", func(t *testing.T) {
		err := appendSummaryToLog("/nonexistent/path", domain_pricing.UsageStats{}, 0, "model")
		require.NoError(t, err)
	})

	t.Run("write to unwritable directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod not effective on Windows")
		}
		tmpDir := t.TempDir()
		require.NoError(t, os.Chmod(tmpDir, 0555))
		t.Cleanup(func() { _ = os.Chmod(tmpDir, 0755) })

		usage := domain_pricing.UsageStats{PromptTokens: 100, ResponseTokens: 50}
		err := appendSummaryToLog(filepath.Join(tmpDir, "sub", "log"), usage, 1.0, "model")
		require.Error(t, err)
	})
}

// =============================================================================
// appendSummaryToLog happy-path test — verifying file content
// =============================================================================

// TestAppendSummaryToLog_WritesFile verifies that non-zero usage results in a
// valid JSON summary line written to the log file with correct field values.
func TestAppendSummaryToLog_WritesFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "tokens.log")

	usage := domain_pricing.UsageStats{
		PromptTokens:   100,
		ResponseTokens: 50,
	}
	err := appendSummaryToLog(logPath, usage, 0.0015, "test-model")
	require.NoError(t, err)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)

	var metrics llm.Metrics
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(data), &metrics))

	if !metrics.IsSummary {
		t.Error("expected IsSummary to be true")
	}
	if metrics.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", metrics.PromptTokens)
	}
	if metrics.ResponseTokens != 50 {
		t.Errorf("expected 50 response tokens, got %d", metrics.ResponseTokens)
	}
	if metrics.TotalTokens != 150 {
		t.Errorf("expected 150 total tokens, got %d", metrics.TotalTokens)
	}
	if metrics.Model != "test-model" {
		t.Errorf("expected model 'test-model', got %q", metrics.Model)
	}
	if metrics.Cost != 0.0015 {
		t.Errorf("expected cost 0.0015, got %f", metrics.Cost)
	}
}

// =============================================================================
// writeTraceEntry table-driven tests — covering direct I/O boundaries
// =============================================================================

// TestWriteTraceEntry exercises writeTraceEntry (metrics.go:276-294), the
// fire-and-forget helper that opens, writes, and closes a trace file. All
// errors are logged via slog.Warn, never returned.
func TestWriteTraceEntry(t *testing.T) {
	t.Run("successful write", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		traceFile := filepath.Join(tmpDir, "trace.jsonl")

		tl := NewTraceLogger(nil)
		tl.writeTraceEntry(traceFile, []byte(`{"status":"ok"}`))

		data, err := os.ReadFile(traceFile)
		require.NoError(t, err)
		require.Equal(t, "{\"status\":\"ok\"}\n", string(data))
	})

	t.Run("open error", func(t *testing.T) {
		t.Parallel()

		spy := &testfixtures.SpyLogger{}
		tl := NewTraceLogger(newSpySlogLogger(spy))
		tl.writeTraceEntry("/nonexistent/dir/subdir/file", []byte(`{}`))

		require.True(t, spy.CalledWith("Warn", "failed to open trace file"),
			"expected slog.Warn 'failed to open trace file' to be logged")
	})

	t.Run("write error /dev/full", func(t *testing.T) {
		if _, err := os.Stat("/dev/full"); os.IsNotExist(err) {
			t.Skip("/dev/full does not exist on this system")
		}

		spy := &testfixtures.SpyLogger{}
		tl := NewTraceLogger(newSpySlogLogger(spy))
		tl.writeTraceEntry("/dev/full", []byte(`{}`))

		require.True(t, spy.CalledWith("Warn", "failed to write to trace file"),
			"expected slog.Warn 'failed to write to trace file' to be logged")
	})

	t.Run("close error", func(t *testing.T) {
		t.Parallel()

		spy := &testfixtures.SpyLogger{}
		tl := &TraceLogger{
			marshalFunc: json.Marshal,
			openTraceFile: func(path string) (io.WriteCloser, error) {
				return &errCloser{Writer: &bytes.Buffer{}}, nil
			},
			logger: newSpySlogLogger(spy),
		}

		tl.writeTraceEntry(t.TempDir()+"/dummy.jsonl", []byte(`{}`))

		require.True(t, spy.CalledWith("Warn", "failed to close trace file"),
			"expected slog.Warn 'failed to close trace file' to be logged")
	})
}
