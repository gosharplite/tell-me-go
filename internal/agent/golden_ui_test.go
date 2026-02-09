// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestUIRendererGolden(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	renderer := NewStdUIRenderer(sm)

	var stdout, stderr bytes.Buffer
	renderer.SetWriters(&stdout, &stderr)

	fixedTime, _ := time.Parse("15:04:05", "12:00:00")

	t.Run("LogTurnStatus_PreCall", func(t *testing.T) {
		stderr.Reset()
		renderer.LogTurnStatus(events.TurnStatus{
			Timestamp:        fixedTime,
			CurrentTurns:     1,
			MaxHistoryTurns:  20,
			Tokens:           1234,
			MaxHistoryTokens: 100000,
			IsPostCall:       false,
		})
		verifyGolden(t, "turn_status_pre.golden", stderr.String())
	})

	t.Run("LogTurnStatus_PostCall", func(t *testing.T) {
		stderr.Reset()
		renderer.LogTurnStatus(events.TurnStatus{
			Timestamp:        fixedTime,
			CurrentTurns:     1,
			MaxHistoryTurns:  20,
			Tokens:           1234,
			MaxHistoryTokens: 100000,
			IsPostCall:       true,
			StartTime:        fixedTime.Add(-5 * time.Second),
			SessionCost:      0.0123,
			DailyCost:        0.0543,
			TaskCost:         0.0045,
			TotalM:           500,
			TotalH:           1000,
			TotalO:           200,
			Metrics: &llm.Metrics{
				PromptTokens:   1500,
				CachedTokens:   1000,
				ResponseTokens: 200,
				TotalTokens:    1700,
				Duration:       1.5,
				ToolDuration:   0.5,
				Cost:           0.0032,
			},
		})
		verifyGolden(t, "turn_status_post.golden", stderr.String())
	})

	t.Run("LogTurnStatus_PostCall_Cliff", func(t *testing.T) {
		stderr.Reset()
		renderer.LogTurnStatus(events.TurnStatus{
			Timestamp:        fixedTime,
			CurrentTurns:     1,
			MaxHistoryTurns:  20,
			Tokens:           130000,
			MaxHistoryTokens: 100000,
			TieredThreshold:  128000,
			IsPostCall:       true,
			StartTime:        fixedTime.Add(-5 * time.Second),
			Metrics: &llm.Metrics{
				PromptTokens:   130000,
				CachedTokens:   120000,
				ResponseTokens: 5000,
				TotalTokens:    135000,
				Duration:       1.5,
			},
		})
		verifyGolden(t, "turn_status_cliff.golden", stderr.String())
	})

	t.Run("LogTurnStatus_PostCall_Warning", func(t *testing.T) {
		stderr.Reset()
		renderer.LogTurnStatus(events.TurnStatus{
			Timestamp:        fixedTime,
			CurrentTurns:     1,
			MaxHistoryTurns:  20,
			Tokens:           105000,
			MaxHistoryTokens: 100000,
			TieredThreshold:  128000,
			IsPostCall:       true,
			StartTime:        fixedTime.Add(-5 * time.Second),
			Metrics: &llm.Metrics{
				PromptTokens:   105000,
				CachedTokens:   100000,
				ResponseTokens: 2000,
				TotalTokens:    107000,
				Duration:       1.5,
			},
		})
		verifyGolden(t, "turn_status_warning.golden", stderr.String())
	})

	t.Run("LogToolResult", func(t *testing.T) {
		stderr.Reset()
		// Mock the time in the function by using a regex replacement in verifyGolden
		renderer.LogToolResult("list_files", tools.ToolResult{Text: "file1.go\nfile2.go"}, true)
		verifyGolden(t, "tool_result.golden", stderr.String())
	})
}

func verifyGolden(t *testing.T, goldenFile, actual string) {
	t.Helper()

	// Normalize timestamps for determinism
	reTime := regexp.MustCompile(`\d{2}:\d{2}:\d{2}`)
	actual = reTime.ReplaceAllString(actual, "[TIME]")

	// Normalize durations which might vary slightly due to time.Since
	reDuration := regexp.MustCompile(`\d+\.\d+s`)
	actual = reDuration.ReplaceAllString(actual, "[DUR]s")

	goldenPath := filepath.Join("testdata", goldenFile)

	if os.Getenv("UPDATE_GOLDEN") == "true" {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatalf("failed to create testdata directory: %v", err)
		}
		err := os.WriteFile(goldenPath, []byte(actual), 0644)
		if err != nil {
			t.Fatalf("failed to update golden file: %v", err)
		}
	}

	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("failed to read golden file: %v. Run with UPDATE_GOLDEN=true to create it.", err)
	}

	if string(expected) != actual {
		t.Errorf("output does not match golden file %s\nDiff:\nExpected:\n%s\nActual:\n%s", goldenFile, string(expected), actual)
	}
}
