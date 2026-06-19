// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_telemetry "github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Gap 1 — RecordSessionCost error from appendSummaryToLog (metrics.go:141-143)
// =============================================================================

// TestRecordSessionCost_AppendSummaryError exercises the error return path
// where appendSummaryToLog fails because openLogFileForAppend cannot open
// the log file for writing.
//
// Strategy:
//  1. Create a directory layout with a valid log file and pricing data.
//  2. First call to RecordSessionCost verifies the happy path.
//  3. Make the log file read-only (0444) so openLogFileForAppend fails
//     with EACCES when attempting O_WRONLY|O_APPEND. The output directory
//     remains writable so EstimateCost and AtomicWrite continue to work.
//  4. Second call asserts the error propagates correctly and that the
//     global ledger WAS updated (proving EstimateCost succeeded before
//     appendSummaryToLog failed).
func TestRecordSessionCost_AppendSummaryError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod not effective on Windows")
	}

	tempDir := t.TempDir()

	// Directory layout:
	//   <tmpdir>/output/session_tokens.log  (valid JSON with tokens)
	//   <tmpdir>/assets/pricing.json        (valid pricing data)
	outputDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	assetsDir := filepath.Join(tempDir, "assets")
	require.NoError(t, os.MkdirAll(assetsDir, 0755))

	logPath := filepath.Join(outputDir, "session_tokens.log")

	// Write a valid log entry with non-zero tokens so parseUsage succeeds
	// and appendSummaryToLog does not hit the zero-usage early return.
	// Use a recent timestamp so the retention policy (30-day cutoff)
	// does not drop the record from the ledger.
	recentTS := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	logContent := `{"prompt_tokens":100,"response_tokens":50,"timestamp":"` + recentTS + `"}` + "\n"
	require.NoError(t, os.WriteFile(logPath, []byte(logContent), 0644))

	// Write valid pricing data matching the model used in the log.
	pricingContent := `{
		"updated_at": "2023-10-27T00:00:00Z",
		"models": {
			"test-model": {
				"hit": 0.5,
				"miss": 1.0,
				"comp": 2.0
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(assetsDir, "pricing.json"), []byte(pricingContent), 0644))

	// Pre-create empty global_costs.json so loadHistory does NOT trigger
	// async ledger recovery. This avoids a race between the background
	// recovery goroutine and subsequent RecordSessionCost calls that
	// manifests when tests run in the full suite.
	historyPath := filepath.Join(tempDir, "global_costs.json")
	require.NoError(t, os.WriteFile(historyPath, []byte("[]"), 0644))

	// Restore permissions if the test fails midway.
	t.Cleanup(func() {
		_ = os.Chmod(logPath, 0644)
	})

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)

	// ---- First call: happy path ----
	err := RecordSessionCost(context.Background(), sm, nil, logPath, "test-model", "manual", "session-1", nil)
	require.NoError(t, err, "first RecordSessionCost should succeed (happy path)")

	// Read the current state of global_costs.json to compare after second call.
	firstLedger, err := os.ReadFile(historyPath)
	require.NoError(t, err)

	// ---- Make log file read-only ----
	// File write permission controls writing to the file itself.
	// 0444 on the file prevents O_WRONLY|O_APPEND on an existing file.
	// The output directory remains writable so AtomicWrite (which writes
	// to tempDir/global_costs.json) and EstimateCost (which uses os.Open,
	// read-only) continue to work.
	require.NoError(t, os.Chmod(logPath, 0444))

	// ---- Second call: appendSummaryToLog should fail ----
	err = RecordSessionCost(context.Background(), sm, nil, logPath, "test-model", "manual", "session-2", nil)
	require.Error(t, err, "second RecordSessionCost should fail when log file is read-only")

	// The error propagates from openLogFileForAppend through appendSummaryToLog.
	// openLogFileForAppend wraps with "failed to open log file ... for summary append".
	errStr := err.Error()
	assert.True(t,
		strings.Contains(errStr, "failed to open log file") || strings.Contains(errStr, "permission denied"),
		"error should indicate open failure, got: %v", err)

	// Restore permissions so we can read.
	require.NoError(t, os.Chmod(logPath, 0644))

	// Verify global_costs.json WAS updated (proving EstimateCost succeeded
	// before appendSummaryToLog failed).
	secondLedger, err := os.ReadFile(historyPath)
	require.NoError(t, err)
	assert.NotEqual(t, string(firstLedger), string(secondLedger),
		"global_costs.json should have been updated by the second call "+
			"(EstimateCost succeeds before appendSummaryToLog is called)")
}

// =============================================================================
// Gap 2 — appendSummaryToLog WriteString error (metrics.go:224-227)
// =============================================================================

// TestAppendSummaryToLog_WriteErrorUnreachable documents that the
// fAppend.WriteString error path inside appendSummaryToLog is UNREACHABLE
// at the unit-test level. WriteString on a regular file opened with
// O_WRONLY|O_APPEND only fails on disk-full or hardware failure —
// integration-level concerns that are structurally unreachable without
// filesystem mocking.
//
// This test follows the established unreachable-documentation pattern
// (see TestUpdateLedgerHistory_MarshalErrorUnreachable).
func TestAppendSummaryToLog_WriteErrorUnreachable(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "summary.log")

	// 1. Happy path: verify appendSummaryToLog succeeds with non-zero usage.
	usage := domain_pricing.UsageStats{
		PromptTokens:   100,
		ResponseTokens: 50,
	}

	err := appendSummaryToLog(logPath, usage, 1.5, "test-model")
	require.NoError(t, err, "appendSummaryToLog should succeed with non-zero usage")

	// Verify the file was created and contains valid JSON.
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.True(t, len(data) > 0, "log file should not be empty")

	var written llm.Metrics
	require.NoError(t, json.Unmarshal(data, &written),
		"written content should be valid JSON")
	assert.Equal(t, int32(100), written.PromptTokens)
	assert.Equal(t, int32(50), written.ResponseTokens)
	assert.Equal(t, 1.5, written.Cost)
	assert.True(t, written.IsSummary, "summary flag should be set")

	// 2. Prove llm.Metrics always marshals cleanly — marshal a fully
	//    populated Metrics struct and verify round-trip.
	now := time.Now().Format(time.RFC3339)
	fullMetrics := llm.Metrics{
		Timestamp:              now,
		Provider:               "test-provider",
		Model:                  "test-model",
		CachedTokens:           100,
		CacheWriteTokens:       20,
		PromptTokens:           200,
		ResponseTokens:         300,
		TotalTokens:            500,
		ThinkingTokens:         50,
		SearchQueries:          3,
		Duration:               1.5,
		ToolDuration:           0.3,
		CumulativeToolDuration: 0.8,
		Cost:                   2.5,
		IsSummary:              true,
		TrafficType:            "batch",
	}

	marshaled, err := json.Marshal(fullMetrics)
	require.NoError(t, err, "llm.Metrics with all fields must marshal cleanly")

	var restored llm.Metrics
	require.NoError(t, json.Unmarshal(marshaled, &restored),
		"round-trip unmarshal must succeed")
	assert.Equal(t, fullMetrics.PromptTokens, restored.PromptTokens)
	assert.Equal(t, fullMetrics.Cost, restored.Cost)
	assert.Equal(t, fullMetrics.SearchQueries, restored.SearchQueries)
	assert.Equal(t, fullMetrics.Model, restored.Model)

	// 3. Verify appendSummaryToLog handles extreme-but-valid UsageStats
	//    (max int32 values, since they are cast to int32 in llm.Metrics).
	extremeUsage := domain_pricing.UsageStats{
		PromptTokens:     math.MaxInt32,
		ResponseTokens:   math.MaxInt32,
		CachedTokens:     math.MaxInt32,
		CacheWriteTokens: math.MaxInt32,
		SearchQueries:    math.MaxInt32,
		ThinkingTokens:   math.MaxInt32,
	}

	extremeLogPath := filepath.Join(tmpDir, "extreme_summary.log")
	err = appendSummaryToLog(extremeLogPath, extremeUsage, 999999.99, "extreme-model")
	require.NoError(t, err, "appendSummaryToLog should succeed with extreme UsageStats")

	// Verify extreme summary is valid JSON.
	extremeData, err := os.ReadFile(extremeLogPath)
	require.NoError(t, err)
	var extremeWritten llm.Metrics
	require.NoError(t, json.Unmarshal(extremeData, &extremeWritten),
		"extreme summary should be valid JSON")
	assert.True(t, extremeWritten.IsSummary)
	assert.Equal(t, 999999.99, extremeWritten.Cost)

	// NOTE: WriteString on a regular file opened with O_WRONLY|O_APPEND
	// only fails on disk-full or hardware failure. These are integration-
	// level concerns that cannot be triggered at the unit-test level
	// without mocking the filesystem (e.g., using afero or similar).
	// The error-handling branch at metrics.go:224-227 is therefore
	// structurally unreachable in unit tests. This is consistent with
	// the project's approach to other similarly unreachable paths
	// (e.g., json.Marshal errors on standard JSON types).
}

// =============================================================================
// Gap 2b — appendSummaryToLog json.Marshal error (metrics.go:213-215)
// =============================================================================

// TestAppendSummaryToLog_MarshalError proves that the json.Marshal error path
// in appendSummaryToLog is reachable when llm.Metrics float64 fields contain
// NaN or Inf. Unlike TurnTrace (all fields are JSON-safe types), llm.Metrics
// has float64 fields (Cost, Duration, ToolDuration, CumulativeToolDuration)
// that can hold NaN/Inf values, causing json.Marshal to fail with
// *json.UnsupportedValueError.
//
// This test follows the same structural pattern as the existing gap tests:
// direct proof of reachability, then exercise the production code path.
func TestAppendSummaryToLog_MarshalError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "summary.log")

	// Usage with non-zero tokens so appendSummaryToLog does not early-return.
	usage := domain_pricing.UsageStats{
		PromptTokens:   100,
		ResponseTokens: 50,
	}

	// -------------------------------------------------------------------------
	// 1. Prove json.Marshal fails on NaN — the key distinction from TurnTrace.
	//    TurnTrace fields are all string/time.Time/time.Duration (JSON-safe).
	//    llm.Metrics has float64 fields that CAN contain NaN/Inf.
	// -------------------------------------------------------------------------
	_, err := json.Marshal(llm.Metrics{Cost: math.NaN()})
	require.Error(t, err, "json.Marshal should fail when Cost is NaN")
	var unsupportedValueErr *json.UnsupportedValueError
	require.ErrorAs(t, err, &unsupportedValueErr,
		"error should be *json.UnsupportedValueError")

	// Also prove +Inf and -Inf fail.
	_, err = json.Marshal(llm.Metrics{Duration: math.Inf(1)})
	require.Error(t, err, "json.Marshal should fail when Duration is +Inf")
	_, err = json.Marshal(llm.Metrics{ToolDuration: math.Inf(-1)})
	require.Error(t, err, "json.Marshal should fail when ToolDuration is -Inf")

	// -------------------------------------------------------------------------
	// 2. Exercise appendSummaryToLog with NaN cost — marshal fails internally.
	// -------------------------------------------------------------------------
	err = appendSummaryToLog(logPath, usage, math.NaN(), "test-model")
	require.Error(t, err, "appendSummaryToLog should fail with NaN cost")
	require.ErrorContains(t, err, "failed to marshal cost summary",
		"error should wrap with 'failed to marshal cost summary'")
	require.ErrorContains(t, err, "json: unsupported value",
		"underlying error should be the json unsupported value error")

	// -------------------------------------------------------------------------
	// 3. Happy-path reinforcement: valid cost produces valid JSON.
	// -------------------------------------------------------------------------
	validPath := filepath.Join(tmpDir, "valid_summary.log")
	err = appendSummaryToLog(validPath, usage, 1.5, "test-model")
	require.NoError(t, err, "appendSummaryToLog should succeed with valid cost")

	data, err := os.ReadFile(validPath)
	require.NoError(t, err)
	require.True(t, len(data) > 0, "log file should not be empty")

	var written llm.Metrics
	require.NoError(t, json.Unmarshal(data, &written),
		"written content should be valid JSON")
	assert.Equal(t, int32(100), written.PromptTokens)
	assert.Equal(t, int32(50), written.ResponseTokens)
	assert.Equal(t, 1.5, written.Cost)
	assert.True(t, written.IsSummary, "summary flag should be set")
}

// =============================================================================
// Gap 3 — logTrace Write error (metrics.go:264)
// =============================================================================

// TestLogTrace_WriteErrorUnreachable documents that the f.Write error path
// inside logTrace is UNREACHABLE at the unit-test level. f.Write on a
// regular file only fails on disk-full or hardware failure. json.Marshal
// on TurnTrace is also unreachable (all fields are standard JSON types;
// sync.Mutex is excluded via json:"-"). The f.Close error path is already
// covered by TestLogTrace_WriteError_DevFull in
// log_trace_write_error_test.go.
//
// This test follows the established unreachable-documentation pattern
// (see TestUpdateLedgerHistory_MarshalErrorUnreachable).
func TestLogTrace_WriteErrorUnreachable(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	traceFile := filepath.Join(tmpDir, "trace.jsonl")

	// 1. Happy path: verify logTrace succeeds with a valid TurnTrace and
	//    writable file.
	trace := &domain_telemetry.TurnTrace{
		StartTime:         time.Now(),
		EndTime:           time.Now().Add(time.Second),
		InferenceDuration: 500 * time.Millisecond,
		ToolExecutions: []domain_telemetry.ToolExecutionTrace{
			{
				ToolName:  "search",
				StartTime: time.Now(),
				Duration:  200 * time.Millisecond,
				Status:    "success",
			},
			{
				ToolName:  "read_file",
				StartTime: time.Now(),
				Duration:  100 * time.Millisecond,
				Status:    "success",
				Error:     "",
			},
		},
		FinalStatus: "completed",
	}

	logTrace(context.Background(), traceFile, trace)

	// Verify the file was created and contains valid JSON.
	data, err := os.ReadFile(traceFile)
	require.NoError(t, err)
	require.True(t, len(data) > 0, "trace file should not be empty")

	// Trim the trailing newline added by logTrace.
	line := strings.TrimSuffix(string(data), "\n")
	var restored domain_telemetry.TurnTrace
	require.NoError(t, json.Unmarshal([]byte(line), &restored),
		"trace file should contain valid JSON")
	assert.Equal(t, trace.FinalStatus, restored.FinalStatus)
	assert.Equal(t, len(trace.ToolExecutions), len(restored.ToolExecutions))

	// 2. Prove domain_telemetry.TurnTrace always marshals cleanly —
	//    marshal a fully populated TurnTrace and verify round-trip.
	fullTrace := &domain_telemetry.TurnTrace{
		StartTime:         time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
		EndTime:           time.Date(2025, 1, 15, 12, 0, 5, 0, time.UTC),
		InferenceDuration: 3 * time.Second,
		ToolExecutions: []domain_telemetry.ToolExecutionTrace{
			{
				ToolName:  "tool_a",
				StartTime: time.Date(2025, 1, 15, 12, 0, 1, 0, time.UTC),
				Duration:  1 * time.Second,
				Status:    "success",
				Error:     "",
			},
			{
				ToolName:  "tool_b",
				StartTime: time.Date(2025, 1, 15, 12, 0, 2, 0, time.UTC),
				Duration:  2 * time.Second,
				Status:    "circuit_open",
				Error:     "circuit breaker tripped",
			},
		},
		FinalStatus: "error",
	}

	marshaled, err := json.Marshal(fullTrace)
	require.NoError(t, err, "TurnTrace with all fields must marshal cleanly")

	var rt domain_telemetry.TurnTrace
	require.NoError(t, json.Unmarshal(marshaled, &rt),
		"round-trip unmarshal of TurnTrace must succeed")
	assert.Equal(t, fullTrace.FinalStatus, rt.FinalStatus)
	assert.Equal(t, len(fullTrace.ToolExecutions), len(rt.ToolExecutions))
	assert.Equal(t, fullTrace.ToolExecutions[0].ToolName, rt.ToolExecutions[0].ToolName)
	assert.Equal(t, fullTrace.ToolExecutions[1].Error, rt.ToolExecutions[1].Error)

	// 3. Verify logTrace doesn't panic with zero-value TurnTrace fields.
	zeroTrace := &domain_telemetry.TurnTrace{}
	zeroFile := filepath.Join(tmpDir, "zero_trace.jsonl")
	// Must not panic.
	logTrace(context.Background(), zeroFile, zeroTrace)

	zeroData, err := os.ReadFile(zeroFile)
	require.NoError(t, err)
	require.True(t, len(zeroData) > 0, "zero-value trace should produce output")

	// NOTE: f.Write on a regular file only fails on disk-full or hardware
	// failure. json.Marshal on TurnTrace is unreachable because all fields
	// are standard JSON types (sync.Mutex excluded via json:"-"). Both are
	// integration-level concerns that cannot be triggered at the unit-test
	// level without filesystem mocking. The f.Close error path is covered
	// by TestLogTrace_WriteError_DevFull in log_trace_write_error_test.go.
}

// =============================================================================
// Gap — resolveUsageForSummary parseUsage error (metrics.go:139-141)
// =============================================================================

// TestResolveUsageForSummary_ParseUsageError covers the gap at metrics.go:139-141
// where resolveUsageForSummary calls parseUsage (when tracker is nil) and
// parseUsage returns a non-ErrNotExist error. The error is propagated to the
// caller. This is the path exercised inside RecordSessionCost when the log
// file exists but cannot be read.
//
// Strategy:
//  1. Create a valid pricing.json so GetPricing succeeds.
//  2. Create a tokens.log, then chmod 0000 to make it unreadable.
//  3. Call resolveUsageForSummary directly — parseUsage fails with EACCES.
//  4. Assert the error is returned, not silently swallowed.
func TestResolveUsageForSummary_ParseUsageError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0000 not effective on Windows")
	}

	tempDir := t.TempDir()

	// Setup pricing data so GetPricing succeeds.
	logDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(logDir, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "assets"), 0755))
	pricingContent := `{"updated_at":"2025-06-15T00:00:00Z","models":{"test-model":{"hit":0.5,"miss":1.0,"comp":2.0}}}`
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "assets", "pricing.json"), []byte(pricingContent), 0644))

	logPath := filepath.Join(logDir, "session_tokens.log")
	// Write valid log content so the file exists.
	logContent := `{"prompt_tokens":100,"response_tokens":50,"cost":0.01,"timestamp":"2025-06-15T12:00:00Z"}` + "\n"
	require.NoError(t, os.WriteFile(logPath, []byte(logContent), 0644))

	// Make the log file unreadable.
	require.NoError(t, os.Chmod(logPath, 0000))
	t.Cleanup(func() { _ = os.Chmod(logPath, 0644) })

	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath(tempDir)

	// Call resolveUsageForSummary directly with tracker=nil.
	// parseUsage calls os.Open(path) which fails with permission denied.
	// This is NOT os.ErrNotExist, so resolveUsageForSummary returns the error.
	usage, cost, err := resolveUsageForSummary(context.Background(), sm, nil, logPath, "test-model", nil)

	assert.Error(t, err, "expected error when log file is unreadable")
	assert.Contains(t, err.Error(), "failed to parse usage log for summary",
		"error should wrap with 'failed to parse usage log for summary'")
	assert.Empty(t, usage.PromptTokens)
	assert.Equal(t, float64(0), cost)
}
