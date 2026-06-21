// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSessionCostTracker_CalculateDailyCost(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("UTC-8", -8*3600)
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, loc) // Jan 2nd, 2026 Noon UTC-8
	currentSessionID := "interactive/session1.tokens.log"

	tests := []struct {
		name          string
		totalCost     float64
		records       []sessionCostRecord
		expectedDaily float64
	}{
		{
			name:          "Empty Ledger",
			totalCost:     1.50,
			records:       []sessionCostRecord{},
			expectedDaily: 1.50,
		},
		{
			name:      "Same Day, Different Session",
			totalCost: 1.50,
			records: []sessionCostRecord{
				{
					Session:   "interactive/session0.tokens.log",
					TotalCost: 2.00,
					Timestamp: now.Add(-1 * time.Hour),
				},
			},
			expectedDaily: 3.50,
		},
		{
			name:      "Same Day, Current Session Already Logged",
			totalCost: 1.50,
			records: []sessionCostRecord{
				{
					Session:   currentSessionID,
					TotalCost: 0.50, // Old value in ledger
					Timestamp: now.Add(-1 * time.Hour),
				},
				{
					Session:   "interactive/session0.tokens.log",
					TotalCost: 2.00,
					Timestamp: now.Add(-2 * time.Hour),
				},
			},
			expectedDaily: 3.50, // 1.50 (current) + 2.00 (other)
		},
		{
			name:      "Different Day (Yesterday)",
			totalCost: 1.50,
			records: []sessionCostRecord{
				{
					Session:   "interactive/yesterday.tokens.log",
					TotalCost: 10.00,
					Timestamp: now.Add(-24 * time.Hour),
				},
			},
			expectedDaily: 1.50,
		},
		{
			name:      "UTC-8 Boundary - Just before midnight UTC",
			totalCost: 1.50,
			// Jan 3rd 05:00 UTC is Jan 2nd 21:00 UTC-8
			records: []sessionCostRecord{
				{
					Session:   "interactive/late_session.tokens.log",
					TotalCost: 5.00,
					Timestamp: time.Date(2026, 1, 3, 5, 0, 0, 0, time.UTC),
				},
			},
			expectedDaily: 6.50,
		},
		{
			name:      "UTC-8 Boundary - Just after midnight UTC-8",
			totalCost: 1.50,
			// Jan 3rd 08:00 UTC is Jan 3rd 00:00 UTC-8
			records: []sessionCostRecord{
				{
					Session:   "interactive/next_day_early.tokens.log",
					TotalCost: 5.00,
					Timestamp: time.Date(2026, 1, 3, 8, 0, 0, 0, time.UTC),
				},
			},
			expectedDaily: 1.50, // Should ignore next day
		},
		{
			name:      "Backward Compatibility - Date string only",
			totalCost: 1.50,
			records: []sessionCostRecord{
				{
					Session:   "interactive/legacy.tokens.log",
					Date:      "2026-01-02",
					TotalCost: 3.00,
				},
			},
			expectedDaily: 4.50,
		},
		{
			name:      "Invalid Record - No date or timestamp",
			totalCost: 1.50,
			records: []sessionCostRecord{
				{
					Session:   "bad_record",
					TotalCost: 100.0,
				},
			},
			expectedDaily: 1.50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tracker := &sessionCostTracker{
				totalCost: tt.totalCost,
			}
			got := tracker.calculateDailyCost(tt.records, now, currentSessionID)
			if got != tt.expectedDaily {
				t.Errorf("calculateDailyCost() = %v, want %v", got, tt.expectedDaily)
			}
		})
	}
}

func TestSessionIDConsistency(t *testing.T) {
	t.Parallel()
	mode := "interactive"
	logFile := "/path/to/some/dir/2026-01-02-12-00-tokens.log"

	expected := "interactive/2026-01-02-12-00-tokens.log"

	// Test internal helper directly
	generated := generateSessionID(mode, logFile)
	if generated != expected {
		t.Errorf("generateSessionID() = %q, want %q", generated, expected)
	}

	// Test consistency between Tracker and Manager initialization paths
	tracker := &sessionCostTracker{
		mode:    mode,
		logFile: logFile,
	}

	manager := &metricsManager{
		mode:    mode,
		logFile: logFile,
	}

	// Simulate the logic used in GetDailyCost
	trackerID := generateSessionID(tracker.mode, tracker.logFile)

	// Simulate the logic used in EstimateCost
	managerID := generateSessionID(manager.mode, manager.logFile)

	if trackerID != managerID {
		t.Errorf("Tracker ID %q does not match Manager ID %q", trackerID, managerID)
	}

	if trackerID != expected {
		t.Errorf("Final generated ID %q does not match expected %q", trackerID, expected)
	}
}

// ---------------------------------------------------------------------------
// GetDailyCost extended tests (Phase 6)
// ---------------------------------------------------------------------------

func TestGetDailyCost_RecoveryInProgress(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "mode")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(outputDir, "tokens.log")
	// Create tokens.log so logFile is not empty
	if err := os.WriteFile(logFile, []byte(`{}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	historyPath := filepath.Join(tempDir, "global_costs.json")

	// Simulate recovery in progress.
	recoveryInProgress.Store(historyPath, true)
	t.Cleanup(func() { recoveryInProgress.Delete(historyPath) })

	tracker := &sessionCostTracker{
		totalCost: 5.0,
		logFile:   logFile,
		mode:      "mode",
	}

	got := tracker.GetDailyCost(context.Background())
	if got != 5.0 {
		t.Errorf("expected in-memory cost 5.0 during recovery, got %f", got)
	}
}

func TestGetDailyCost_ReadFileNonNotExist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows: os.Chmod(0000) does not prevent reading for the owner.")
	}
	// NOT parallel — chmod cleanup sensitive.
	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "mode")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(outputDir, "tokens.log")
	if err := os.WriteFile(logFile, []byte(`{}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	historyPath := filepath.Join(tempDir, "global_costs.json")
	// Create file with mode 0000 so os.ReadFile fails with permission denied.
	if err := os.WriteFile(historyPath, []byte("valid"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(historyPath, 0644) })

	tracker := &sessionCostTracker{
		totalCost: 7.5,
		logFile:   logFile,
		mode:      "mode",
	}

	got := tracker.GetDailyCost(context.Background())
	if got != 7.5 {
		t.Errorf("expected in-memory cost 7.5 for unreadable ledger, got %f", got)
	}
}

func TestGetDailyCost_UnmarshalError(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "mode")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(outputDir, "tokens.log")
	if err := os.WriteFile(logFile, []byte(`{}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	historyPath := filepath.Join(tempDir, "global_costs.json")
	// Write invalid JSON that json.Unmarshal will reject.
	if err := os.WriteFile(historyPath, []byte("not valid {{{ json"), 0644); err != nil {
		t.Fatal(err)
	}

	tracker := &sessionCostTracker{
		totalCost: 12.0,
		logFile:   logFile,
		mode:      "mode",
	}

	got := tracker.GetDailyCost(context.Background())
	if got != 12.0 {
		t.Errorf("expected in-memory cost 12.0 for invalid JSON ledger, got %f", got)
	}
}

func TestGetDailyCost_CachedPathAfterFirstCall(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "mode")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(outputDir, "tokens.log")
	if err := os.WriteFile(logFile, []byte(`{"model": "test", "prompt_tokens": 100, "response_tokens": 50}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pre-populate the ledger with a different session's cost for today.
	loc := time.FixedZone("UTC-8", -8*3600)
	now := time.Now().In(loc)
	historyPath := filepath.Join(tempDir, "global_costs.json")
	records := []sessionCostRecord{
		{
			Session:   "mode/other_session.tokens.log",
			TotalCost: 3.0,
			Timestamp: now,
		},
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tracker := &sessionCostTracker{
		totalCost:        2.0,
		logFile:          logFile,
		mode:             "mode",
		currentSessionID: generateSessionID("mode", logFile),
	}

	// First call: cache is empty, triggers refresh from ledger.
	first := tracker.GetDailyCost(context.Background())
	expected := 5.0 // 3.0 (other session) + 2.0 (current)
	if first != expected {
		t.Errorf("first GetDailyCost() = %v, want %v", first, expected)
	}

	// Now remove the ledger file to prove the second call does NOT read from disk.
	if err := os.Remove(historyPath); err != nil {
		t.Fatal(err)
	}

	// Second call: cached path, should return same value despite missing ledger.
	second := tracker.GetDailyCost(context.Background())
	if second != expected {
		t.Errorf("second GetDailyCost() = %v, want %v (cached, no disk read)", second, expected)
	}

	// Verify cachedExternalDailyCost is set.
	tracker.mu.Lock()
	if tracker.cachedExternalDailyCost != 3.0 {
		t.Errorf("cachedExternalDailyCost = %v, want 3.0", tracker.cachedExternalDailyCost)
	}
	tracker.mu.Unlock()
}

func TestWarmup_PopulatesDailyCostCache(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "mode")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(outputDir, "tokens.log")
	// Write a session log so ensureInitialized() succeeds.
	if err := os.WriteFile(logFile, []byte(`{"model": "test", "prompt_tokens": 100, "response_tokens": 50}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Pre-populate the ledger.
	loc := time.FixedZone("UTC-8", -8*3600)
	now := time.Now().In(loc)
	historyPath := filepath.Join(tempDir, "global_costs.json")
	records := []sessionCostRecord{
		{
			Session:   "mode/other.tokens.log",
			TotalCost: 7.5,
			Timestamp: now,
		},
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tracker := &sessionCostTracker{
		logFile: logFile,
		mode:    "mode",
	}

	// Warmup should populate both the stats/totalCost and the daily cache.
	tracker.Warmup()

	tracker.mu.Lock()
	if tracker.cachedDate == "" {
		t.Error("expected cachedDate to be set after Warmup()")
	}
	// External cost should be 7.5 (other session) and totalCost from parseUsage.
	if tracker.cachedExternalDailyCost != 7.5 {
		t.Errorf("cachedExternalDailyCost = %v, want 7.5", tracker.cachedExternalDailyCost)
	}
	tracker.mu.Unlock()

	// GetDailyCost should use the cache (not re-read the ledger).
	if err := os.Remove(historyPath); err != nil {
		t.Fatal(err)
	}
	daily := tracker.GetDailyCost(context.Background())
	expected := 7.5 + tracker.totalCost // external + current
	if daily != expected {
		t.Errorf("GetDailyCost after Warmup = %v, want %v", daily, expected)
	}
}

func TestGetDailyCost_DateRolloverRefreshesCache(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, "mode")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(outputDir, "tokens.log")
	if err := os.WriteFile(logFile, []byte(`{"model": "test", "prompt_tokens": 10, "response_tokens": 5}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Ledger: one record for today, one for tomorrow.
	loc := time.FixedZone("UTC-8", -8*3600)
	now := time.Now().In(loc)
	tomorrow := now.Add(24 * time.Hour)

	historyPath := filepath.Join(tempDir, "global_costs.json")
	records := []sessionCostRecord{
		{
			Session:   "mode/today_other.tokens.log",
			TotalCost: 4.0,
			Timestamp: now,
		},
		{
			Session:   "mode/tomorrow_other.tokens.log",
			TotalCost: 99.0,
			Timestamp: tomorrow,
		},
	}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	tracker := &sessionCostTracker{
		totalCost:        1.0,
		logFile:          logFile,
		mode:             "mode",
		currentSessionID: generateSessionID("mode", logFile),
	}

	// Prime the cache with yesterday's date so today triggers a refresh.
	tracker.mu.Lock()
	tracker.cachedDate = now.Add(-24 * time.Hour).Format("2006-01-02")
	tracker.cachedExternalDailyCost = 999.0 // stale value
	tracker.mu.Unlock()

	// GetDailyCost: cachedDate != today, so refresh.
	daily := tracker.GetDailyCost(context.Background())

	// Should be 4.0 (today_other) + 1.0 (current) = 5.0.
	// The 99.0 from tomorrow and 999.0 stale cache should be ignored.
	expected := 5.0
	if daily != expected {
		t.Errorf("GetDailyCost() = %v, want %v (date rollover should refresh)", daily, expected)
	}

	// Verify the cache was updated.
	tracker.mu.Lock()
	if tracker.cachedExternalDailyCost != 4.0 {
		t.Errorf("cachedExternalDailyCost = %v, want 4.0 (refreshed)", tracker.cachedExternalDailyCost)
	}
	tracker.mu.Unlock()
}
