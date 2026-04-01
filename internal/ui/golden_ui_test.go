// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

var update = flag.Bool("update", false, "update golden files")

func TestUIRendererGolden(t *testing.T) {
	var stdout, stderr bytes.Buffer
	locker := &mockLocker{}
	// Inject deterministic dependencies
	frozenTime := time.Date(2026, 1, 1, 15, 4, 5, 0, time.UTC)
	mc := &mockClock{now: frozenTime}
	r := NewRenderer(locker, &stdout, &stderr, mc, nil).(*stdUIRenderer)

	r.SetUseColor(true)

	// Ensure testdata directory exists
	testDataDir := filepath.Join("testdata")
	if err := os.MkdirAll(testDataDir, 0755); err != nil {
		t.Fatalf("failed to create testdata directory: %v", err)
	}

	t.Run("Pre-Call Status", func(t *testing.T) {
		stderr.Reset()
		stdout.Reset()

		status := events.TurnStatus{
			Timestamp:        frozenTime,
			SessionTurns:     2, // Will show as 3/10
			MaxHistoryTurns:  10,
			Tokens:           1250,
			MaxHistoryTokens: 10000,
			IsPostCall:       false,
		}

		r.LogTurnStatus(context.Background(), status)

		verifyGolden(t, "turn_status_pre.golden", stderr.String())
	})

	t.Run("Post-Call Status", func(t *testing.T) {
		stderr.Reset()
		stdout.Reset()

		status := events.TurnStatus{
			Timestamp:       frozenTime,
			SessionTurns:    2,
			MaxHistoryTurns: 10,
			IsPostCall:      true,
			IsFinal:         true,
			StartTime:       frozenTime.Add(-10 * time.Second),
			SessionCost:     1.2345,
			DailyCost:       5.6789,
			TaskCost:        0.0123,
			TotalM:          1000,
			TotalH:          2000,
			TotalO:          500,
			Metrics: &llm.Metrics{
				Provider:               "deepseek",
				PromptTokens:           1200,
				CachedTokens:           800,
				ResponseTokens:         300,
				ThinkingTokens:         100,
				Duration:               5.5,
				ToolDuration:           2.5,
				CumulativeToolDuration: 4.5,
				Cost:                   0.0050,
			},
		}

		r.LogTurnStatus(context.Background(), status)

		verifyGolden(t, "turn_status_post.golden", stderr.String())
	})
}

func verifyGolden(t *testing.T, filename, actual string) {
	t.Helper()
	goldenPath := filepath.Join("testdata", filename)

	if *update {
		if err := os.WriteFile(goldenPath, []byte(actual), 0644); err != nil {
			t.Fatalf("failed to update golden file %s: %v", filename, err)
		}
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("golden file %s does not exist. Run with -update to create it.", filename)
		}
		t.Fatalf("failed to read golden file %s: %v", filename, err)
	}

	if actual != string(expected) {
		t.Errorf("output mismatch for %s\nActual:\n%s\nExpected:\n%s", filename, actual, string(expected))
	}
}
