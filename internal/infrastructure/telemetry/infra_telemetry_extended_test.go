// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
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

type mockKV struct {
	ports.KVStore
	val string
	err error
}

func (m *mockKV) Get(ctx context.Context, key string) (string, error) {
	return m.val, m.err
}

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

	if err := RegisterMetrics(reg, sm, logFile, traceFile, "test-model", "test-mode", nil, nil); err != nil {
		t.Fatalf("RegisterMetrics failed: %v", err)
	}

	if _, ok := reg.handlers["estimate_cost"]; !ok {
		t.Error("estimate_cost tool not registered")
	}
	if _, ok := reg.handlers["get_cost_summary"]; !ok {
		t.Error("get_cost_summary tool not registered")
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

	t.Run("Call get_cost_summary", func(t *testing.T) {
		t.Parallel()
		handler := reg.handlers["get_cost_summary"]

		// Create a ledger file
		historyPath := filepath.Join(tempDir, "global_costs.json")
		history := []sessionCostRecord{
			{Date: "2026-01-01", Session: "s1", TotalCost: 1.0, Model: "test-model"},
		}
		data, _ := json.Marshal(history)
		_ = os.WriteFile(historyPath, data, 0644)

		res, err := handler(context.Background(), nil, nil)
		if err != nil {
			t.Fatalf("get_cost_summary failed: %v", err)
		}
		if res.Text == "" {
			t.Error("get_cost_summary returned empty result")
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

	err := RecordSessionCost(context.Background(), sm, tracker, logFile, "test-model", "test-mode", "test-session", nil)
	if err != nil {
		t.Fatalf("RecordSessionCost failed: %v", err)
	}

	// Verify ledger
	historyPath := filepath.Join(tempDir, "global_costs.json")
	if _, err := os.Stat(historyPath); os.IsNotExist(err) {
		t.Error("global_costs.json not created in parent of output dir")
	}
}

func TestTraceTelemetry(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	traceFile := filepath.Join(tempDir, "test.trace.jsonl")

	trace := &domain_telemetry.TurnTrace{
		FinalStatus: "success",
	}

	logTrace(context.Background(), traceFile, trace)

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

func TestLedger_Extended(t *testing.T) {
	t.Parallel()
	t.Run("IsStale", func(t *testing.T) {
		t.Parallel()
		tempFile := filepath.Join(t.TempDir(), "stale.lock")
		_ = os.WriteFile(tempFile, []byte(""), 0644)

		if isStale(tempFile) {
			t.Error("New file should not be stale")
		}

		oldTime := time.Now().Add(-10 * time.Minute)
		_ = os.Chtimes(tempFile, oldTime, oldTime)

		if !isStale(tempFile) {
			t.Error("Old file should be stale")
		}
	})

	t.Run("FindLogFiles", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		subDir := filepath.Join(tempDir, "subdir")
		_ = os.Mkdir(subDir, 0755)

		logPath := filepath.Join(subDir, "session_tokens.log")
		_ = os.WriteFile(logPath, []byte("data"), 0644)

		ls := &ledgerStore{}
		files, err := ls.findLogFiles(tempDir)
		if err != nil {
			t.Fatal(err)
		}

		found := false
		for _, f := range files {
			if filepath.Base(f) == "session_tokens.log" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected to find session_tokens.log")
		}
	})

	t.Run("AcquireAndReleaseLock", func(t *testing.T) {
		t.Parallel()
		tempDir := t.TempDir()
		historyPath := filepath.Join(tempDir, "global_costs.json")

		ls := &ledgerStore{}
		f, err := acquireLedgerLock(historyPath + ".lock")
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}

		// Try to acquire again
		f2, err := acquireLedgerLock(historyPath + ".lock")
		if err == nil {
			_ = f2.Close()
			t.Error("Should not be able to acquire lock again")
		}

		ls.releaseLedgerLock(historyPath, f)

		// Should be able to acquire now
		f3, err := acquireLedgerLock(historyPath + ".lock")
		if err != nil {
			t.Errorf("Failed to acquire lock after release: %v", err)
		}
		ls.releaseLedgerLock(historyPath, f3)
	})
}

func TestMetricsManager_LoadHistory_Corrupted(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	historyPath := filepath.Join(tempDir, "global_costs.json")
	_ = os.WriteFile(historyPath, []byte("invalid json"), 0644)

	m := &metricsManager{}
	history := m.loadHistory(context.Background(), historyPath, tempDir)

	if len(history) != 0 {
		t.Error("Expected empty history for corrupted file")
	}

	if _, err := os.Stat(historyPath + ".bak"); os.IsNotExist(err) {
		t.Error("Backup file should be created for corrupted ledger")
	}
}

func TestMetricsManager_Retention(t *testing.T) {
	t.Parallel()
	m := &metricsManager{}
	now := time.Now()
	history := []sessionCostRecord{
		{Date: now.Format("2006-01-02"), Session: "new"},
		{Date: now.AddDate(0, 0, -40).Format("2006-01-02"), Session: "old"},
	}

	filtered := m.applyRetentionPolicy(history, 30)
	if len(filtered) != 1 {
		t.Errorf("Expected 1 record after retention, got %d", len(filtered))
	}
	if filtered[0].Session != "new" {
		t.Error("Kept wrong record")
	}
}

func TestMetricsManager_LoadRetentionDays(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &metricsManager{}

	// Case 1: No KVStore
	if days := m.loadRetentionDays(ctx); days != 30 {
		t.Errorf("Expected default 30 days when KVStore is nil, got %d", days)
	}

	// Case 2: KVStore with retention days
	m.kvStore = &mockKV{val: "60"}
	if days := m.loadRetentionDays(ctx); days != 60 {
		t.Errorf("Expected 60 days, got %d", days)
	}

	// Case 3: KVStore with invalid value
	m.kvStore = &mockKV{val: "abc"}
	if days := m.loadRetentionDays(ctx); days != 30 {
		t.Errorf("Expected default 30 days for invalid value, got %d", days)
	}
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

func TestIsStale_NonExistent(t *testing.T) {
	t.Parallel()
	if isStale("/nonexistent/path/to/lock") {
		t.Error("Non-existent file should not be stale")
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
	logTrace(context.Background(), "", trace)

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
	logTrace(context.Background(), traceFile, nil)

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
	logTrace(ctx, traceFile, trace)

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
	logTrace(context.Background(), traceFile, trace)

	// Verify the file was NOT created.
	if _, err := os.Stat(traceFile); !os.IsNotExist(err) {
		t.Error("expected no file for open error path, but file exists")
	}
}

func TestLogTrace_WriteError(t *testing.T) {
	// NOT parallel — uses FIFO coordination that is timing-sensitive.
	fifoPath := filepath.Join(t.TempDir(), "fifo")

	if err := syscall.Mkfifo(fifoPath, 0666); err != nil {
		t.Skipf("cannot create fifo (non-Unix?): %v", err)
	}

	trace := &domain_telemetry.TurnTrace{FinalStatus: "fifo-test"}

	// Strategy: reader opens (unblocks writer's open), then immediately closes.
	// When logTrace then calls Write, the reader is gone → EPIPE.
	readerOpened := make(chan struct{})
	readerClosed := make(chan struct{})

	go func() {
		r, err := os.OpenFile(fifoPath, os.O_RDONLY, 0)
		if err != nil {
			close(readerOpened)
			return
		}
		close(readerOpened) // signal: reader connected (both opens completed)
		_ = r.Close()       // immediately close — writer gets EPIPE on next write
		close(readerClosed)
	}()

	logDone := make(chan struct{})
	go func() {
		logTrace(context.Background(), fifoPath, trace)
		close(logDone)
	}()

	// Wait for reader to connect (unblocks logTrace's open).
	<-readerOpened
	// Wait for reader to fully close before logTrace marshals and writes.
	<-readerClosed
	// Now logTrace will marshal and write — reader is gone, so f.Write gets EPIPE.
	<-logDone
	// If we reach here, the write-error path was exercised (log.Printf warning).
}

func TestLogTrace_CloseError(t *testing.T) {
	// NOT parallel — uses RLIMIT_FSIZE to trigger EFBIG on write, and
	// attempts to also trigger a close(2) error.
	//
	// Strategy:
	//  1. Set RLIMIT_FSIZE to a small value (1KB).
	//  2. Create a large TurnTrace whose JSON exceeds the limit.
	//  3. logTrace opens (succeeds), writes → EFBIG (write-error path).
	//  4. close(2) after a failed O_APPEND write: on Linux/ext4 this
	//     typically returns 0, so the close-error body is unreachable
	//     in practice. We retain this test to prove the path exists and
	//     document the limitation.

	tempDir := t.TempDir()
	traceFile := filepath.Join(tempDir, "close_err.jsonl")

	// Build a trace whose JSON exceeds ~1KB.
	execs := make([]domain_telemetry.ToolExecutionTrace, 200)
	for i := range execs {
		execs[i] = domain_telemetry.ToolExecutionTrace{
			ToolName:  "very-long-tool-name-to-fill-bytes",
			StartTime: time.Now(),
			Duration:  time.Second,
			Status:    "success",
			Error:     "a detailed error message that takes up space in the JSON output",
		}
	}
	trace := &domain_telemetry.TurnTrace{
		StartTime:         time.Now(),
		EndTime:           time.Now().Add(time.Minute),
		InferenceDuration: 30 * time.Second,
		ToolExecutions:    execs,
		FinalStatus:       "complete",
	}

	var orig syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &orig); err != nil {
		t.Skipf("cannot get rlimit: %v", err)
	}
	defer func() { _ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &orig) }()

	lim := orig
	lim.Cur = 1024 // 1KB — trace JSON is ~30KB
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &lim); err != nil {
		t.Skipf("cannot set rlimit: %v", err)
	}

	// logTrace will open, marshal, write → EFBIG, then close.
	// Write-error path is covered. Close-error path depends on kernel/fs.
	logTrace(context.Background(), traceFile, trace)

	// Verify the file was created (it should exist but be truncated).
	if fi, err := os.Stat(traceFile); err == nil {
		t.Logf("file size after EFBIG write: %d bytes", fi.Size())
	}
}

// TestLogTrace_MarshalErrorUnreachable documents that the json.Marshal error
// path in logTrace is unreachable because TurnTrace contains only JSON-
// serializable fields (sync.Mutex excluded via json:"-"). No test is needed.
func TestLogTrace_MarshalErrorUnreachable(t *testing.T) {
	t.Parallel()

	// Verify TurnTrace serializes cleanly with all field types.
	trace := &domain_telemetry.TurnTrace{
		StartTime:         time.Now(),
		EndTime:           time.Now().Add(time.Second),
		InferenceDuration: 500 * time.Millisecond,
		ToolExecutions: []domain_telemetry.ToolExecutionTrace{
			{ToolName: "tool1", StartTime: time.Now(), Duration: time.Second, Status: "success"},
		},
		FinalStatus: "complete",
	}

	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("unexpected marshal error (TurnTrace should always be serializable): %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty JSON output")
	}

	// Also verify the zero-value TurnTrace serializes.
	data2, err := json.Marshal(&domain_telemetry.TurnTrace{})
	if err != nil {
		t.Fatalf("unexpected marshal error for zero TurnTrace: %v", err)
	}
	if len(data2) == 0 {
		t.Error("expected non-empty JSON output for zero TurnTrace")
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

	f, err := openLogFileForAppend(logPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Verify the file is open and writable.
	if _, err := f.WriteString("new line\n"); err != nil {
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

	f, err := openLogFileForAppend(logPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = f.Close() }()

	// Verify the directory was created.
	if _, err := os.Stat(filepath.Dir(logPath)); os.IsNotExist(err) {
		t.Error("parent directory was not created")
	}

	// Verify we can write to the file.
	if _, err := f.WriteString("hello\n"); err != nil {
		t.Fatalf("write failed: %v", err)
	}
}

func TestOpenLogFileForAppend_MkdirAllFails(t *testing.T) {
	t.Parallel()

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

	_, err := openLogFileForAppend(logPath)
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

	tmpDir := t.TempDir()
	// Create the parent directory but make it non-writable.
	parentDir := filepath.Join(tmpDir, "locked")
	if err := os.Mkdir(parentDir, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parentDir, 0755) })

	logPath := filepath.Join(parentDir, "log.jsonl")

	_, err := openLogFileForAppend(logPath)
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

// TestOpenLogFileForAppend_MkdirAllSucceedsButRetryFails covers the branch
// where the first OpenFile fails with ENOENT, MkdirAll succeeds, but the
// retry OpenFile still fails. We use syscall.Umask(0o777) so that MkdirAll
// creates the parent directory with mode 0000, causing the retry to fail
// with EACCES.
//
// NOT parallel — syscall.Umask is process-global.
func TestOpenLogFileForAppend_MkdirAllSucceedsButRetryFails(t *testing.T) {
	// NOT parallel: umask is process-global.
	tmpDir := t.TempDir()

	// Only ONE level of missing directory so MkdirAll itself succeeds.
	logPath := filepath.Join(tmpDir, "missing", "log.jsonl")

	// Set umask so MkdirAll creates the parent dir with mode 0000.
	oldUmask := syscall.Umask(0o777)
	t.Cleanup(func() { syscall.Umask(oldUmask) })

	_, err := openLogFileForAppend(logPath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify the error is from the retry (after mkdir), not from mkdir itself.
	errStr := err.Error()
	if !strings.Contains(errStr, "after mkdir") {
		t.Errorf("error should mention 'after mkdir', got: %v", err)
	}
	// Verify the directory WAS created (MkdirAll succeeded).
	if _, statErr := os.Stat(filepath.Dir(logPath)); os.IsNotExist(statErr) {
		t.Error("parent directory was not created by MkdirAll")
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
	err := appendSummaryToLog(logPath, domain_pricing.UsageStats{}, 0.0, "model")
	if err != nil {
		t.Errorf("expected nil error for zero usage, got: %v", err)
	}

	// Verify no file was created (early return skips I/O).
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Error("expected no log file to be created for zero usage")
	}
}

// TestAppendSummaryToLog_MarshalErrorUnreachable documents gap #2: the
// json.Marshal error path inside appendSummaryToLog is UNREACHABLE because
// llm.Metrics contains only JSON-serializable fields (int32, int, float64,
// string, bool). No test can trigger this branch without violating the Go
// type system.
func TestAppendSummaryToLog_MarshalErrorUnreachable(t *testing.T) {
	t.Parallel()

	// Prove that all field types in llm.Metrics serialize cleanly.
	summary := llm.Metrics{
		Timestamp:      time.Now().Format(time.RFC3339),
		Model:          "test-model",
		CachedTokens:   100,
		PromptTokens:   200,
		ResponseTokens: 300,
		TotalTokens:    600,
		SearchQueries:  5,
		Cost:           1.5,
		IsSummary:      true,
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("unexpected marshal error (llm.Metrics should always be serializable): %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty JSON output")
	}

	// Verify round-trip works.
	var restored llm.Metrics
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}
	if restored.PromptTokens != 200 || restored.Cost != 1.5 {
		t.Errorf("round-trip mismatch: %+v", restored)
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

	err := appendSummaryToLog("/dev/full", usage, 1.0, "model")
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

	err := appendSummaryToLog(logPath, usage, 1.0, "model")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// The error comes from openLogFileForAppend, passed through unwrapped.
	if !strings.Contains(err.Error(), "failed to open log file") {
		t.Errorf("error should contain open failure prefix, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// findLogFiles extended tests (Phase 6)
// ---------------------------------------------------------------------------

// setupFindLogFilesWithUnreadableSubdir creates a temp directory containing:
//   - readable/tokens.log (valid)
//   - locked/              (mode 0000, contains tokens.log)
//
// Returns the root directory. Skips in short mode or if chmod is unavailable.
// Caller must call t.Cleanup to restore permissions; this helper registers
// the cleanup itself.
func setupFindLogFilesWithUnreadableSubdir(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping chmod-based test in short mode")
	}

	tempDir := t.TempDir()

	// Create a valid tokens.log in a readable subdirectory.
	validDir := filepath.Join(tempDir, "readable")
	if err := os.MkdirAll(validDir, 0755); err != nil {
		t.Fatal(err)
	}
	validLog := filepath.Join(validDir, "tokens.log")
	if err := os.WriteFile(validLog, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an unreadable subdirectory so WalkDir's ReadDir fails
	// with a non-IsNotExist error, exercising the callback's error branch.
	badDir := filepath.Join(tempDir, "locked")
	if err := os.MkdirAll(badDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(badDir, 0000); err != nil {
		t.Skipf("cannot chmod directory (maybe root?): %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(badDir, 0755) })

	return tempDir
}

func TestFindLogFiles_UnreadableSubdirectory_CallbackError(t *testing.T) {
	// NOT parallel — chmod on temp dirs can interfere.
	tempDir := setupFindLogFilesWithUnreadableSubdir(t)

	ls := &ledgerStore{}
	files, err := ls.findLogFiles(tempDir)
	// Walk errors are now collected and returned via errors.Join.
	// Expect a non-nil error because of the permission-denied subdirectory.
	if err == nil {
		t.Fatal("findLogFiles should return an error for inaccessible subdirectory")
	}
	if !strings.Contains(err.Error(), "walk errors during recovery") {
		t.Errorf("error should contain 'walk errors during recovery', got: %v", err)
	}

	found := false
	for _, f := range files {
		if filepath.Base(f) == "tokens.log" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find tokens.log despite unreadable subdirectory")
	}
}

func TestFindLogFiles_NonExistentRoot(t *testing.T) {
	t.Parallel()
	ls := &ledgerStore{}
	// WalkDir invokes the callback with os.IsNotExist for non-existent roots,
	// and since the callback returns nil, WalkDir also returns nil.
	files, err := ls.findLogFiles("/nonexistent/path/for/walkdir")
	if err != nil {
		t.Fatalf("expected nil error for non-existent root, got: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected empty files for non-existent root, got %d entries", len(files))
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

	err := RegisterMetrics(reg, sm, logFile, traceFile, "test-model", "test-mode", nil, nil)
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

// TestRegisterMetrics_SecondRegistrationFails covers the error return path
// when get_cost_summary registration fails after estimate_cost succeeded
// (line 65-117 of metrics.go). This locks in the current partial-state
// behavior: estimate_cost IS registered but get_cost_summary is NOT.
func TestRegisterMetrics_SecondRegistrationFails(t *testing.T) {
	t.Parallel()

	reg := &failingRegistry{
		mockRegistry: &mockRegistry{},
		failOn:       "get_cost_summary",
	}
	sm := &mockSM{}

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "output")
	_ = os.Mkdir(outputDir, 0755)
	logFile := filepath.Join(outputDir, "test.log")
	traceFile := filepath.Join(outputDir, "test.trace.jsonl")

	err := RegisterMetrics(reg, sm, logFile, traceFile, "test-model", "test-mode", nil, nil)
	if err == nil {
		t.Fatal("expected error from second registration, got nil")
	}
	if !strings.Contains(err.Error(), "injected failure for get_cost_summary") {
		t.Errorf("error should mention get_cost_summary failure, got: %v", err)
	}

	// Verify partial state: estimate_cost IS registered.
	if _, ok := reg.handlers["estimate_cost"]; !ok {
		t.Error("estimate_cost should be registered (first registration succeeded)")
	}

	// Verify partial state: get_cost_summary is NOT registered.
	if _, ok := reg.handlers["get_cost_summary"]; ok {
		t.Error("get_cost_summary should NOT be registered (second registration failed)")
	}
}

// ---------------------------------------------------------------------------
// RegisterMetrics handler closure coverage (Phase 8)
// ---------------------------------------------------------------------------

// TestRegisterMetrics_UnmarshalArgsErrorUnreachable documents that the
// UnmarshalArgs error paths inside the RegisterMetrics closures are unreachable
// because both estimateCostArgs and costSummaryArgs are empty structs.
// json.Marshal of any map[string]interface{} always succeeds and json.Unmarshal
// into an empty struct always succeeds regardless of JSON content — unknown
// fields are silently ignored. No test can trigger this branch without
// violating the Go type system.
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
		{"billing": true, "start_date": "2026-01-01"},
	} {
		if err := tools.UnmarshalArgs(args, &eArgs); err != nil {
			t.Fatalf("unexpected error for estimateCostArgs with %v: %v", args, err)
		}
	}

	// Prove costSummaryArgs always unmarshals cleanly.
	// Extra/unknown fields are silently ignored by encoding/json.
	for _, args := range []map[string]interface{}{
		nil,
		{},
		{"billing": true},
		{"start_date": "2026-01-01", "end_date": "2026-01-31", "interval": "day", "group_by": "model"},
		{"unknown_field": 123, "billing": false},
		{"interval": "hour", "group_by": "date,model", "billing": true},
	} {
		var sArgs costSummaryArgs
		if err := tools.UnmarshalArgs(args, &sArgs); err != nil {
			t.Fatalf("unexpected error for costSummaryArgs with %v: %v", args, err)
		}
	}

	// Verify that costSummaryArgs correctly captures known fields.
	var sArgs costSummaryArgs
	_ = tools.UnmarshalArgs(map[string]interface{}{
		"billing":    true,
		"start_date": "2026-06-01",
		"end_date":   "2026-06-30",
		"interval":   "hour",
		"group_by":   "model",
	}, &sArgs)
	if !sArgs.Billing || sArgs.Interval != "hour" || sArgs.GroupBy != "model" {
		t.Errorf("costSummaryArgs fields not populated correctly: %+v", sArgs)
	}
}

// setupGetCostSummaryHandlerWithSilentUpdateError creates a directory layout
// where parseUsage fails with EACCES (logFile inside no-permission dir) but
// getCostSummary succeeds (global_costs.json in writable parent).
// It registers metrics and returns the get_cost_summary handler.
// Skips in short mode or if chmod is unavailable.
func setupGetCostSummaryHandlerWithSilentUpdateError(t *testing.T) tools.ToolFunc {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping chmod-based test in short mode")
	}

	reg := &mockRegistry{}
	sm := &mockSM{}
	tmpDir := t.TempDir()

	// Create a writable parent directory so global_costs.json can be placed
	// at the correct location for getCostSummary to find.
	parentDir := filepath.Join(tmpDir, "parent")
	if err := os.Mkdir(parentDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a no-permission subdirectory. Setting logFile inside this dir
	// causes os.Open (inside parseUsage) to fail with EACCES, which is NOT
	// os.IsNotExist. EstimateCost returns a non-nil error, triggering the
	// log.Printf warning branch in the get_cost_summary handler closure.
	noAccessDir := filepath.Join(parentDir, "noaccess")
	if err := os.Mkdir(noAccessDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(noAccessDir, 0000); err != nil {
		t.Skipf("cannot chmod (maybe root?): %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(noAccessDir, 0755) })

	logFile := filepath.Join(noAccessDir, "session_tokens.log")
	traceFile := filepath.Join(tmpDir, "trace.jsonl")

	// Place global_costs.json where getCostSummary expects it:
	//   outputDir = filepath.Dir(logFile)  = parentDir/noaccess
	//   globalDir = filepath.Dir(outputDir) = parentDir
	//   historyPath = parentDir/global_costs.json
	historyPath := filepath.Join(parentDir, "global_costs.json")
	records := []sessionCostRecord{
		{Date: "2026-01-15", Session: "test-session", TotalCost: 1.5, Model: "test-model"},
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := RegisterMetrics(reg, sm, logFile, traceFile, "test-model", "test-mode", nil, nil); err != nil {
		t.Fatalf("RegisterMetrics failed: %v", err)
	}

	handler := reg.handlers["get_cost_summary"]
	if handler == nil {
		t.Fatal("get_cost_summary handler not registered")
	}
	return handler
}

// TestRegisterMetrics_GetCostSummaryHandler_SilentUpdateError exercises the
// get_cost_summary handler closure body (metrics.go:69-116), specifically
// the log.Printf warning path (lines 83-85) when EstimateCost fails with
// a non-NotExist error. We make parseUsage fail by placing the logFile inside
// a no-permission directory so os.Open returns EACCES.
//
// NOT parallel — uses chmod on a temp directory.
func TestRegisterMetrics_GetCostSummaryHandler_SilentUpdateError(t *testing.T) {
	// NOT parallel — uses chmod on a temp directory.
	handler := setupGetCostSummaryHandlerWithSilentUpdateError(t)

	// Call the handler. The silent update (EstimateCost) will fail because
	// parseUsage cannot open logFile (EACCES on parent dir). The handler
	// logs a warning but continues to getCostSummary, which reads the
	// pre-created global_costs.json and returns a valid summary.
	res, err := handler(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("get_cost_summary handler failed: %v", err)
	}
	if res.Text == "" {
		t.Error("expected non-empty cost summary result despite silent update failure")
	}
	// The summary table (default date-grouped) shows cost data, not model name.
	// Verify the cost from our test record appears.
	if !strings.Contains(res.Text, "$1.5000") {
		t.Errorf("summary should contain $1.5000, got: %s", res.Text)
	}
}
