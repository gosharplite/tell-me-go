// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domain_telemetry "github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Mock Security Manager
type mockSM struct {
	domain_security.Manager
}

func (m *mockSM) IsPathSafe(path string) (string, error) {
	return path, nil
}

func (m *mockSM) IsPathWritable(path string) (string, error) {
	return path, nil
}

func (m *mockSM) Close() error { return nil }

func (m *mockSM) IsBypassActive() bool { return false }

// Mock Tool Registry
type mockRegistry struct {
	tools.Registry
	handlers map[string]tools.ToolFunc
}

func (m *mockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	if m.handlers == nil {
		m.handlers = make(map[string]tools.ToolFunc)
	}
	m.handlers[def.Name] = handler
	return nil
}

func (m *mockRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	if m.handlers == nil {
		m.handlers = make(map[string]tools.ToolFunc)
	}
	m.handlers[def.Name] = handler
	return nil
}

// failingRegistry extends mockRegistry with optional error injection fields
// for testing RegisterMetrics error paths.
type failingRegistry struct {
	*mockRegistry // POINTER embedding
	registerErr   error
	failOn        string
}

func (f *failingRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	if f.registerErr != nil {
		return f.registerErr
	}
	if f.failOn != "" && def.Name == f.failOn {
		return errors.New("injected failure for " + def.Name)
	}
	return f.mockRegistry.RegisterWithOptions(def, handler, opts)
}

func TestSessionCostTracker_Extended(t *testing.T) {
	t.Parallel()
	sm := &mockSM{}
	pricing := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"test-model": {Hit: 0.1, Miss: 1.0, Comp: 2.0},
		},
	}

	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	tracker := NewSessionCostTracker(sm, logFile, "test-mode", "test-model", pricing.Models["test-model"], pricing)

	t.Run("Warmup", func(t *testing.T) {
		content := `{"model": "test-model", "prompt_tokens": 1000, "response_tokens": 500, "cached_tokens": 100}` + "\n"
		err := os.WriteFile(logFile, []byte(content), 0644)
		if err != nil {
			t.Fatal(err)
		}

		tracker.Warmup()
		stats, cost := tracker.GetStats(context.Background())
		if stats.PromptTokens != 1000 {
			t.Errorf("Expected 1000 prompt tokens, got %d", stats.PromptTokens)
		}
		if cost <= 0 {
			t.Errorf("Expected non-zero cost after warmup")
		}
	})

	t.Run("AccumulateAndReturn", func(t *testing.T) {
		mt := llm.Metrics{
			PromptTokens:   1000,
			ResponseTokens: 500,
			Model:          "test-model",
		}
		cost := tracker.AccumulateAndReturn(mt)
		if cost <= 0 {
			t.Errorf("Expected positive cost from AccumulateAndReturn")
		}

		_, totalCost := tracker.GetStats(context.Background())
		if totalCost < cost {
			t.Errorf("Total cost should be at least turn cost")
		}
	})
}

func TestRegisterMetrics_Extended(t *testing.T) {
	t.Parallel()
	reg := &mockRegistry{}
	sm := &mockSM{}

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	_ = os.Mkdir(outputDir, 0755)
	logFile := filepath.Join(outputDir, "test.log")
	traceFile := filepath.Join(outputDir, "test.trace.jsonl")

	if err := RegisterMetrics(reg, sm, logFile, traceFile, "test-model", "test-mode", nil); err != nil {
		t.Fatalf("RegisterMetrics failed: %v", err)
	}

	if _, ok := reg.handlers["estimate_cost"]; !ok {
		t.Error("estimate_cost tool not registered")
	}

	t.Run("Call estimate_cost", func(t *testing.T) {
		t.Parallel()
		handler := reg.handlers["estimate_cost"]
		// Create log file
		_ = os.WriteFile(logFile, []byte(`{"model": "test-model", "prompt_tokens": 1000, "response_tokens": 500}`+"\n"), 0644)

		res, err := handler(context.Background(), nil, nil)
		if err != nil {
			t.Fatalf("estimate_cost failed: %v", err)
		}
		if res.Text == "" {
			t.Error("estimate_cost returned empty result")
		}
	})
}

func TestRecordSessionCost_Extended(t *testing.T) {
	t.Parallel()
	sm := &mockSM{}
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	_ = os.Mkdir(outputDir, 0755)
	logFile := filepath.Join(outputDir, "test.log")
	_ = os.WriteFile(logFile, []byte(`{"model": "test-model", "prompt_tokens": 1000, "response_tokens": 500}`+"\n"), 0644)

	pricing := domain_pricing.PricingData{
		Models: map[string]domain_pricing.ModelPricing{
			"test-model": {Hit: 0.1, Miss: 1.0, Comp: 2.0},
		},
	}
	tracker := NewSessionCostTracker(sm, logFile, "test-mode", "test-model", pricing.Models["test-model"], pricing)

	err := RecordSessionCost(context.Background(), sm, tracker, logFile, "test-model", "test-mode", nil)
	if err != nil {
		t.Fatalf("RecordSessionCost failed: %v", err)
	}
}

func TestTraceTelemetry(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	traceFile := filepath.Join(tempDir, "test.trace.jsonl")

	trace := &domain_telemetry.TurnTrace{
		FinalStatus: "success",
	}

	tl := newTraceLogger(nil)
	tl.logTrace(context.Background(), traceFile, trace)

	if _, err := os.Stat(traceFile); os.IsNotExist(err) {
		t.Error("trace file not created")
	}

	t.Run("RegisterTraceSubscriber", func(t *testing.T) {
		t.Parallel()
		bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		eventstest.CleanupBus(t, bus)
		RegisterTraceSubscriber(bus, traceFile)

		_ = bus.Publish(context.Background(), events.TraceEvent{Trace: trace})

		// Flush event bus
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = bus.Flush(ctx)

		if _, err := os.Stat(traceFile); os.IsNotExist(err) {
			t.Error("trace file not created via subscriber")
		}
	})
}

func TestResolveUsageForSummary_NoTracker(t *testing.T) {
	t.Parallel()
	sm := &mockSM{}
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "nonexistent.log")

	usage, cost, err := resolveUsageForSummary(context.Background(), sm, nil, logFile, "model", nil)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if usage.PromptTokens != 0 || cost != 0 {
		t.Error("Expected zero usage/cost for nonexistent log")
	}
}

// ---------------------------------------------------------------------------
// logTrace error-path tests — covering the 6 gaps identified in Phase 3.
// Gap 4 (json.Marshal error) is documented as UNREACHABLE because all
// TurnTrace fields are JSON-serializable (sync.Mutex has json:"-", and
// time.Time, time.Duration, []ToolExecutionTrace, string all marshal cleanly).
// ---------------------------------------------------------------------------

func TestLogTrace_EmptyTraceFile(t *testing.T) {
	t.Parallel()

	trace := &domain_telemetry.TurnTrace{FinalStatus: "test"}
	tempDir := t.TempDir()

	// Call with empty traceFile — should silently skip (gap 1).
	tl := newTraceLogger(nil)
	tl.logTrace(context.Background(), "", trace)

	// Verify no file was created in the temp directory.
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no files, got %d entries", len(entries))
	}
}

func TestLogTrace_NilTrace(t *testing.T) {
	t.Parallel()

	traceFile := filepath.Join(t.TempDir(), "nil_trace.jsonl")

	// Call with nil trace — should silently skip (gap 2).
	tl := newTraceLogger(nil)
	tl.logTrace(context.Background(), traceFile, nil)

	// Verify no file was created.
	if _, err := os.Stat(traceFile); !os.IsNotExist(err) {
		t.Error("expected no file for nil trace, but file exists")
	}
}

func TestLogTrace_CancelledContext(t *testing.T) {
	t.Parallel()

	traceFile := filepath.Join(t.TempDir(), "cancelled.jsonl")
	trace := &domain_telemetry.TurnTrace{FinalStatus: "test"}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	// Call with cancelled context — should skip (gap 3).
	tl := newTraceLogger(nil)
	tl.logTrace(ctx, traceFile, trace)

	// Verify no file was created.
	if _, err := os.Stat(traceFile); !os.IsNotExist(err) {
		t.Error("expected no file for cancelled context, but file exists")
	}
}

func TestLogTrace_OpenError(t *testing.T) {
	t.Parallel()

	// Use a path with a non-existent parent directory.
	// O_CREATE only creates the file, not parent directories,
	// so os.OpenFile will fail with "no such file or directory" (gap 5).
	traceFile := filepath.Join(t.TempDir(), "nonexistent_subdir", "trace.jsonl")
	trace := &domain_telemetry.TurnTrace{FinalStatus: "test"}

	// Must not panic.
	tl := newTraceLogger(nil)
	tl.logTrace(context.Background(), traceFile, trace)

	// Verify the file was NOT created.
	if _, err := os.Stat(traceFile); !os.IsNotExist(err) {
		t.Error("expected no file for open error path, but file exists")
	}
}

func (m *mockRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{Serial: m.IsSerial(name), LongRunning: m.IsLongRunning(name)}
}

func (m *mockRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}

func (m *mockRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return nil
}

func (m *mockRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return nil
}

func (m *mockRegistry) ListAvailableToolkits() []string {
	return nil
}

// ---------------------------------------------------------------------------
// openLogFileForAppend tests — covering 4 branches (Phase 4.1)
// ---------------------------------------------------------------------------

func TestOpenLogFileForAppend_Success(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "existing.log")

	// Pre-create the log file so OpenFile succeeds on first attempt.
	if err := os.WriteFile(logPath, []byte("existing line\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := &metricsManager{fs: osFS{}}
	f, err := m.openLogFileForAppend(logPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Verify the file is open and writable.
	if _, err := io.WriteString(f, "new line\n"); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Verify file contains both lines.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "existing line") || !strings.Contains(string(data), "new line") {
		t.Errorf("file content mismatch: got %q", string(data))
	}
}

func TestOpenLogFileForAppend_MkdirAllSucceeds(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	// Path where the parent directory does NOT exist.
	logPath := filepath.Join(tmpDir, "nonexistent", "subdir", "log.jsonl")

	m := &metricsManager{fs: osFS{}}
	f, err := m.openLogFileForAppend(logPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Verify the directory was created.
	if _, err := os.Stat(filepath.Dir(logPath)); os.IsNotExist(err) {
		t.Error("parent directory was not created")
	}

	// Verify we can write to the file.
	if _, err := io.WriteString(f, "hello\n"); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func TestOpenLogFileForAppend_MkdirAllFails(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not prevent file creation on Windows")
	}

	tmpDir := t.TempDir()
	// Create a read+execute (no write) parent directory.
	// This allows path lookup (ENOENT for the first OpenFile) but
	// prevents MkdirAll from creating the missing subdirectory (EACCES).
	parentDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(parentDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parentDir, 0755) })

	logPath := filepath.Join(parentDir, "missing", "log.jsonl")

	m := &metricsManager{fs: osFS{}}
	_, err := m.openLogFileForAppend(logPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// The error MUST contain both mkdirErr and the original err (double-wrapping).
	errStr := err.Error()
	if !strings.Contains(errStr, "also failed to create dir") {
		t.Errorf("error should mention mkdir failure, got: %v", err)
	}
	if !strings.Contains(errStr, "no such file or directory") {
		t.Errorf("error should contain original open error, got: %v", err)
	}
}

func TestOpenLogFileForAppend_PermissionDenied(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not prevent file creation on Windows")
	}

	tmpDir := t.TempDir()
	// Create the parent directory but make it non-writable.
	parentDir := filepath.Join(tmpDir, "locked")
	if err := os.Mkdir(parentDir, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parentDir, 0755) })

	logPath := filepath.Join(parentDir, "log.jsonl")

	m := &metricsManager{fs: osFS{}}
	_, err := m.openLogFileForAppend(logPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify the error is wrapped with the expected prefix.
	if !strings.Contains(err.Error(), "failed to open log file") {
		t.Errorf("error should be wrapped, got: %v", err)
	}
	// Should NOT contain the mkdir-fallback message.
	if strings.Contains(err.Error(), "also failed to create dir") {
		t.Error("error should NOT mention mkdir fallback for non-NotExist errors")
	}
}

// ---------------------------------------------------------------------------
// resolveUsageForSummary error-path tests (Phase 5.1)
// ---------------------------------------------------------------------------

// TestResolveUsageForSummary_ParseUsageNonNotExistError covers the gap where
// parseUsage returns a non-NotExist error (e.g., trying to open a directory).
// This exercises the os.IsNotExist(err) == false branch, which wraps the
// error with "failed to parse usage log for summary".
func TestResolveUsageForSummary_ParseUsageNonNotExistError(t *testing.T) {
	t.Parallel()

	sm := &mockSM{}
	tempDir := t.TempDir()

	// Create a directory (not a file) so os.Open fails with EISDIR (or
	// equivalent "is a directory" error) — a non-NotExist error.
	dirPath := filepath.Join(tempDir, "adir")
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		t.Fatal(err)
	}

	_, _, err := resolveUsageForSummary(context.Background(), sm, nil, dirPath, "model", nil)
	if err == nil {
		t.Fatal("expected error when passing directory as log path, got nil")
	}

	// Must NOT be a NotExist error — it should be wrapped with our prefix.
	if os.IsNotExist(err) {
		t.Errorf("expected non-NotExist error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "failed to parse usage log for summary") {
		t.Errorf("error should contain wrap prefix, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// appendSummaryToLog error-path tests (Phase 5.2)
// ---------------------------------------------------------------------------

// TestAppendSummaryToLog_ZeroUsage covers gap #1: when all usage fields are
// zero, the function returns nil early without touching the filesystem.
func TestAppendSummaryToLog_ZeroUsage(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "nonexistent.log")

	// All-zero UsageStats — should return nil without creating the file.
	m := &metricsManager{fs: osFS{}}
	err := m.appendSummaryToLog(logPath, domain_pricing.UsageStats{}, 0.0, "model")
	if err != nil {
		t.Errorf("expected nil error for zero usage, got: %v", err)
	}

	// Verify no file was created (early return skips I/O).
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Error("expected no log file to be created for zero usage")
	}
}

// TestAppendSummaryToLog_WriteError covers gap #3: when fAppend.WriteString
// fails (e.g., ENOSPC on /dev/full), the error is wrapped and returned.
func TestAppendSummaryToLog_WriteError(t *testing.T) {
	// NOT parallel — uses /dev/full which may be contended.

	if _, err := os.Stat("/dev/full"); os.IsNotExist(err) {
		t.Skip("/dev/full not available on this system")
	}

	// Non-zero usage bypasses the early-return guard.
	usage := domain_pricing.UsageStats{
		PromptTokens: 100,
	}

	m := &metricsManager{fs: osFS{}}
	err := m.appendSummaryToLog("/dev/full", usage, 1.0, "model")
	if err == nil {
		t.Fatal("expected write error on /dev/full, got nil")
	}

	if !strings.Contains(err.Error(), "failed to write cost summary to log") {
		t.Errorf("error should contain wrap prefix, got: %v", err)
	}
}

// TestResolveUsageForSummary_SuccessWithOverrides covers the two remaining
// uncovered branches in resolveUsageForSummary:
//  1. The for-range overrides loop body (overrides with entries)
//  2. The success return path (parseUsage returns no error)
func TestResolveUsageForSummary_SuccessWithOverrides(t *testing.T) {
	t.Parallel()

	sm := &mockSM{}
	tempDir := t.TempDir()

	// Create a valid log file so parseUsage succeeds.
	logPath := filepath.Join(tempDir, "valid.log")
	content := `{"model": "test-model", "prompt_tokens": 100, "response_tokens": 50}` + "\n"
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Non-nil, non-empty overrides to exercise the loop body.
	overrides := map[string]domain_pricing.ModelPricing{
		"custom-model": {Hit: 0.05, Miss: 0.5, Comp: 1.0},
	}

	usage, cost, err := resolveUsageForSummary(context.Background(), sm, nil, logPath, "test-model", overrides)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usage.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", usage.PromptTokens)
	}
	if cost < 0 {
		t.Errorf("expected non-negative cost, got %f", cost)
	}
}

// TestAppendSummaryToLog_OpenError covers the openLogFileForAppend error
// return path inside appendSummaryToLog. A read-only parent directory causes
// os.OpenFile with O_CREATE to fail with EACCES (not ENOENT), which
// openLogFileForAppend wraps and returns.
func TestAppendSummaryToLog_OpenError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod does not prevent file creation on Windows")
	}

	tmpDir := t.TempDir()

	// Create a read-only parent directory (no write permission).
	roDir := filepath.Join(tmpDir, "readonly")
	if err := os.Mkdir(roDir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0755) })

	logPath := filepath.Join(roDir, "log.jsonl")

	// Non-zero usage to pass the early-return guard.
	usage := domain_pricing.UsageStats{PromptTokens: 100}

	m := &metricsManager{fs: osFS{}}
	err := m.appendSummaryToLog(logPath, usage, 1.0, "model")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// The error comes from openLogFileForAppend, passed through unwrapped.
	if !strings.Contains(err.Error(), "failed to open log file") {
		t.Errorf("error should contain open failure prefix, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// RegisterMetrics error-path tests (Phase 7)
// ---------------------------------------------------------------------------

// TestRegisterMetrics_FirstRegistrationFails covers the error return path
// when estimate_cost registration fails (line 52-63 of metrics.go).
func TestRegisterMetrics_FirstRegistrationFails(t *testing.T) {
	t.Parallel()

	reg := &failingRegistry{
		mockRegistry: &mockRegistry{},
		registerErr:  errors.New("injected"),
	}
	sm := &mockSM{}

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	_ = os.Mkdir(outputDir, 0755)
	logFile := filepath.Join(outputDir, "test.log")
	traceFile := filepath.Join(outputDir, "test.trace.jsonl")

	err := RegisterMetrics(reg, sm, logFile, traceFile, "test-model", "test-mode", nil)
	if err == nil {
		t.Fatal("expected error from first registration, got nil")
	}
	if !strings.Contains(err.Error(), "injected") {
		t.Errorf("error should contain 'injected', got: %v", err)
	}

	// Verify NO handlers were registered.
	if len(reg.handlers) != 0 {
		t.Errorf("expected no handlers, got %d: %v", len(reg.handlers), reg.handlers)
	}
}

// ---------------------------------------------------------------------------
// RegisterMetrics handler closure coverage (Phase 8)
// ---------------------------------------------------------------------------

// TestRegisterMetrics_UnmarshalArgsErrorUnreachable documents that the
// UnmarshalArgs error path inside the RegisterMetrics closure is unreachable
// because estimateCostArgs is an empty struct. json.Marshal of any
// map[string]interface{} always succeeds and json.Unmarshal into an empty
// struct always succeeds regardless of JSON content — unknown fields are
// silently ignored. No test can trigger this branch without violating the
// Go type system.
func TestRegisterMetrics_UnmarshalArgsErrorUnreachable(t *testing.T) {
	t.Parallel()

	// Prove estimateCostArgs{} always unmarshals cleanly.
	var eArgs estimateCostArgs
	for _, args := range []map[string]interface{}{
		nil,
		{},
		{"unknown": "value"},
		{"nested": map[string]interface{}{"deep": 42}},
		{"array": []interface{}{1, 2, 3}},
	} {
		if err := tools.UnmarshalArgs(args, &eArgs); err != nil {
			t.Fatalf("unexpected error for estimateCostArgs with %v: %v", args, err)
		}
	}
}
