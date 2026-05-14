// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

func TestStdUIRenderer_BasicLogging(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	t.Run("LogSystemMessage", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		r.LogSystemMessage(context.Background(), "test message", "error")
		if !strings.Contains(stderr.String(), "test message") {
			t.Errorf("expected stderr to contain 'test message', got %q", stderr.String())
		}
	})

	t.Run("LogTurnStatus", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(context.Background(), events.TurnStatus{
			Timestamp:        mc.Now(),
			CurrentTurns:     0,
			SessionTurns:     0,
			MaxHistoryTurns:  10,
			Tokens:           100,
			MaxHistoryTokens: 1000,
			Mode:             "coder",
		})
		output := stderr.String()
		if !strings.Contains(output, "Turn 1/10 - coder") {
			t.Errorf("expected stderr to contain 'Turn 1/10 - coder', got %q", output)
		}
		// Payload line: contains tokens/max, and - coder.
		// Note: contains ~ since it's not a post-call
		if !strings.Contains(output, "Payload:") || !strings.Contains(output, "/1000 tokens - coder") {
			t.Errorf("expected stderr to contain 'Payload: ... /1000 tokens - coder', got %q", output)
		}
		// Check for the trailing newline (visual gap)
		if !strings.HasSuffix(output, "\n\n") {
			t.Errorf("expected stderr to end with double newline for visual gap, got %q", output)
		}
	})

	t.Run("LogTurnStatus_NoMaxHistoryTurns", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(context.Background(), events.TurnStatus{
			Timestamp:        mc.Now(),
			CurrentTurns:     0,
			SessionTurns:     0,
			MaxHistoryTurns:  0,
			Tokens:           100,
			MaxHistoryTokens: 1000,
		})
		output := stderr.String()
		if !strings.Contains(output, "Turn 1") {
			t.Errorf("expected stderr to contain 'Turn 1', got %q", output)
		}
	})

	t.Run("LogUsage", func(t *testing.T) {
		// LogUsage writes to a file

		tmpFile := t.TempDir() + "/usage.log"
		metrics := &llm.Metrics{
			PromptTokens:   10,
			ResponseTokens: 5,
			TotalTokens:    15,
		}
		r.LogUsage(context.Background(), metrics, tmpFile, mc.Now())

		data, err := os.ReadFile(tmpFile)
		if err != nil {
			t.Fatalf("failed to read usage log: %v", err)
		}
		if !strings.Contains(string(data), "\"total_tokens\":15") {
			t.Errorf("expected usage log to contain '\"total_tokens\":15', got %q", string(data))
		}
	})
}

func TestStdUIRenderer_StatusLogging(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	t.Run("LogTurnStatus_PostCall", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(context.Background(), events.TurnStatus{
			Timestamp:       r.NowSafe(),
			CurrentTurns:    1,
			MaxHistoryTurns: 10,
			IsPostCall:      true,
			IsFinal:         true,
			Metrics: &llm.Metrics{
				PromptTokens:   500,
				CachedTokens:   200,
				ResponseTokens: 100,
				TotalTokens:    600,
				Duration:       2.0,
			},
			StartTime: r.NowSafe().Add(-5 * time.Second),
		})
		output := stderr.String()
		if !strings.Contains(output, "Payload:") {
			t.Errorf("expected stderr to contain 'Payload:', got %q", output)
		}
		if !strings.Contains(output, "Ready") {
			t.Errorf("expected stderr to contain 'Ready', got %q", output)
		}
	})
}

func TestStdUIRenderer_ToolLogging(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	t.Run("LogToolCall_WithShowTools", func(t *testing.T) {
		stderr.Reset()
		r.LogToolCall(context.Background(), []*llm.FunctionCall{{Name: "my_tool", Args: map[string]interface{}{"key": "val", "reason": "my intent"}}}, 0, 5, true)
		output := stderr.String()
		if !strings.Contains(output, "Tool Action") || !strings.Contains(output, "my_tool") {
			t.Errorf("expected stderr to contain 'Tool Action' and 'my_tool', got %q", output)
		}
		if !strings.Contains(output, "[Tool Reason] my intent") {
			t.Errorf("expected stderr to contain '[Tool Reason] my intent', got %q", output)
		}
		if strings.Contains(output, "reason: my intent") {
			t.Errorf("expected 'reason' to be removed from arguments list, got %q", output)
		}
	})

	t.Run("LogToolResult_WithShowTools", func(t *testing.T) {
		stderr.Reset()
		r.LogToolResult(context.Background(), "my_tool", tools.ToolResult{Text: "output", BinaryData: []tools.BinaryData{{MIMEType: "image/png", Data: []byte("xyz")}}}, true)
		if !strings.Contains(stderr.String(), "Tool Result") || !strings.Contains(stderr.String(), "image/png") {
			t.Errorf("expected stderr to contain 'Tool Result' and 'image/png', got %q", stderr.String())
		}
	})
}

func TestStdUIRenderer_ResponseRendering(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	t.Run("RenderResponse_Markdown", func(t *testing.T) {
		stdout.Reset()
		content := &llm.Content{Parts: []*llm.Part{{Text: "# Title\nbody"}}}
		r.RenderResponse(context.Background(), content, false, false)
		if !strings.Contains(stdout.String(), "Title") {
			t.Errorf("expected stdout to contain 'Title', got %q", stdout.String())
		}
	})

	t.Run("RenderResponse_Thoughts", func(t *testing.T) {
		stderr.Reset()
		content := &llm.Content{Parts: []*llm.Part{{Text: "I am thinking", IsThought: true}}}
		r.RenderResponse(context.Background(), content, true, false)
		if !strings.Contains(stderr.String(), "Thinking") || !strings.Contains(stderr.String(), "I am thinking") {
			t.Errorf("expected stderr to contain 'Thinking', got %q", stderr.String())
		}
	})
}

func TestStdUIRenderer_Spinner(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	t.Run("StartSpinner", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		r.SetForceSpinner(true)
		stop := r.StartSpinner(ctx)
		if stop == nil {
			t.Fatal("expected stop function, got nil")
		}
		stop()

		if !strings.Contains(stderr.String(), "Thinking...") {
			t.Errorf("expected stderr to contain 'Thinking...', got %q", stderr.String())
		}
	})

	t.Run("StartSpinnerRace", func(t *testing.T) {
		r.SetForceSpinner(true)
		stop := r.StartSpinner(context.Background())

		var wg sync.WaitGroup
		const numGoroutines = 10
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				// Simulate random arrival of stop calls
				time.Sleep(time.Millisecond * 10)
				stop()
			}()
		}

		wg.Wait()
	})

	t.Run("drawLoadingIndicator outputs to stderr", func(t *testing.T) {
		uiState := r.GetUIState()
		stdout.Reset()
		stderr.Reset()
		r.DrawLoadingIndicator(uiState, "X", mc.Now(), " Thinking...", false, nil)
		if !strings.Contains(stderr.String(), "X Thinking...") {
			t.Errorf("expected stderr to contain spinner, got %q", stderr.String())
		}
		if stdout.Len() > 0 {
			t.Errorf("expected stdout to be empty, got %q", stdout.String())
		}
	})

	t.Run("clearLoadingIndicator outputs to stderr", func(t *testing.T) {
		uiState := r.GetUIState()
		stdout.Reset()
		stderr.Reset()
		r.ClearLoadingIndicator(uiState, false)
		if stderr.Len() == 0 {
			t.Error("expected stderr to contain clear sequence")
		}
		if stdout.Len() > 0 {
			t.Errorf("expected stdout to be empty, got %q", stdout.String())
		}
	})
}

func TestLogTurnStatus_Format(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 21, 4, 52, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	r.LogTurnStatus(context.Background(), events.TurnStatus{
		Timestamp:       r.NowSafe(),
		CurrentTurns:    1,
		MaxHistoryTurns: 10,
		IsPostCall:      true,
		Metrics: &llm.Metrics{
			PromptTokens:   9185,
			CachedTokens:   0,
			ResponseTokens: 516,
			ThinkingTokens: 435,
			TotalTokens:    9185 + 516 + 435,
			Duration:       8.12,
		},
		StartTime: r.NowSafe().Add(-8330 * time.Millisecond), // 8.33s total
	})

	output := stderr.String()
	// Check for the specific format: [21:04:52] M: 9185 H: 0 C: 516 Th: 435 [8.12s / 8.33s]
	// We'll check parts to ignore colors.
	parts := []string{
		"[21:04:52]",
		"M: 9185",
		"H: 0",
		"C: 516",
		"Th: 435",
		"8.12s",
		"(ΣT: 0.00s)",
		"/ 8.33s",
	}

	for _, p := range parts {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q, got %q", p, output)
		}
	}

	t.Run("Priority Indicator", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(context.Background(), events.TurnStatus{
			Timestamp:  r.NowSafe(),
			IsPostCall: true,
			Metrics: &llm.Metrics{
				Provider:    "google",
				TrafficType: "ON_DEMAND_PRIORITY",
			},
		})
		output := stderr.String()
		if !strings.Contains(output, "[google-priority]") {
			t.Errorf("expected output to contain [google-priority], got %q", output)
		}
	})

	t.Run("CumulativeToolDuration", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(context.Background(), events.TurnStatus{
			Timestamp:  r.NowSafe(),
			IsPostCall: true,
			Metrics: &llm.Metrics{
				Duration:               5.0,
				ToolDuration:           2.0,
				CumulativeToolDuration: 3.5,
			},
			StartTime: r.NowSafe().Add(-10 * time.Second),
		})
		output := stderr.String()
		// Expected Total Latency: 5.0 + 2.0 = 7.0s
		// Expected Cumulative: (ΣT: 3.50s)
		if !strings.Contains(output, "7.00s") {
			t.Errorf("expected output to contain total latency 7.00s, got %q", output)
		}
		if !strings.Contains(output, "(ΣT: 3.50s)") {
			t.Errorf("expected output to contain cumulative tool duration (ΣT: 3.50s), got %q", output)
		}
	})

	// Check Ready line with aggregates
	r.LogTurnStatus(context.Background(), events.TurnStatus{
		Timestamp:   mc.Now(),
		IsPostCall:  true,
		IsFinal:     true,
		TaskCost:    0.0001,
		SessionCost: 0.1234,
		TotalM:      1000,
		TotalH:      2000,
		TotalO:      3000,
		Metrics: &llm.Metrics{
			PromptTokens: 10, // Just to satisfy printSystemLine
			Cost:         0.0123,
		},
	})
	output = stderr.String()
	if !strings.Contains(output, "$0.0123 $0.0001") || !strings.Contains(output, "$0.1234") || !strings.Contains(output, "66.7%") {
		t.Errorf("expected output to contain turn, task and session cost, got %q", output)
	}
}

func TestStdUIRenderer_Colors(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	t.Run("Green cost in LogTurnStatus", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(context.Background(), events.TurnStatus{
			IsPostCall:   true,
			IsFinal:      true,
			SessionCost:  1.2345,
			Metrics:      &llm.Metrics{PromptTokens: 100},
			SessionTurns: 1,
		})
		output := stderr.String()
		// Green color for cost: \033[0;32m
		if !strings.Contains(output, "\033[0;32m") {
			t.Errorf("expected output to contain green color for cost, got %q", output)
		}
		if !strings.Contains(output, "$1.2345") {
			t.Errorf("expected output to contain session cost $1.2345, got %q", output)
		}
	})

	t.Run("Yellow warning for token usage", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(context.Background(), events.TurnStatus{
			Tokens:           850,
			MaxHistoryTokens: 1000,
			SessionTurns:     1,
		})
		output := stderr.String()
		// Yellow color: \033[0;33m
		if !strings.Contains(output, "\033[0;33m") {
			t.Errorf("expected output to contain yellow color for warning, got %q", output)
		}
	})

	t.Run("Red error for token overflow", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(context.Background(), events.TurnStatus{
			Tokens:           1100,
			MaxHistoryTokens: 1000,
			SessionTurns:     1,
		})
		output := stderr.String()
		// Red color: \033[0;31m
		if !strings.Contains(output, "\033[0;31m") {
			t.Errorf("expected output to contain red color for overflow, got %q", output)
		}
	})
}

func TestStdUIRenderer_ToolMetrics(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	t.Run("Tool metrics omit total duration", func(t *testing.T) {
		stderr.Reset()
		m := &llm.Metrics{
			PromptTokens:   100,
			ResponseTokens: 50,
			Duration:       1.5,
		}
		r.LogToolResult(context.Background(), "test_tool", tools.ToolResult{
			Metadata: map[string]interface{}{"metrics": m},
		}, true)

		output := stderr.String()
		if !strings.Contains(output, "1.50s") {
			t.Errorf("expected output to contain 1.50s, got %q", output)
		}
		if strings.Contains(output, "/") {
			t.Errorf("expected output NOT to contain total duration separator '/', got %q", output)
		}
	})

	t.Run("Regular metrics include total duration", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(context.Background(), events.TurnStatus{
			IsPostCall: true,
			StartTime:  r.NowSafe().Add(-5 * time.Second),
			Metrics: &llm.Metrics{
				PromptTokens: 100,
				Duration:     1.5,
			},
		})
		output := stderr.String()
		if !strings.Contains(output, "1.50s") || !strings.Contains(output, "5.00s") || !strings.Contains(output, "/") {
			t.Errorf("expected output to contain 1.50s / 5.00s, got %q", output)
		}
	})
}

func TestStdUIRenderer_Concurrency(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	r := ui.NewRenderer(locker, stdout, stderr, clock.RealClock{}, nil).(*ui.StdUIRenderer)

	const (
		numGoroutines = 50
		numIterations = 20
	)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				content := &llm.Content{
					Parts: []*llm.Part{
						{Text: fmt.Sprintf("G%d-I%d-P1 ", id, j)},
						{Text: fmt.Sprintf("G%d-I%d-P2\n", id, j)},
					},
				}
				// Test RenderResponse
				r.RenderResponse(context.Background(), content, false, true)

				// Test LogSystemMessage
				r.LogSystemMessage(context.Background(), fmt.Sprintf("G%d-I%d-Sys", id, j), "info")
			}
		}(i)
	}

	wg.Wait()

	// If the race detector is enabled, it will catch any issues here.
	// We can also do a basic check that we didn't crash and got some output.
	if stdout.Len() == 0 {
		t.Error("expected some stdout output")
	}
	if stderr.Len() == 0 {
		t.Error("expected some stderr output")
	}
}

func TestStdUIRenderer_GetTimestamp(t *testing.T) {
	locker := ui.NewMockLocker()

	t.Run("Mocked time", func(t *testing.T) {
		mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 34, 56, 0, time.UTC))
		r := ui.NewRenderer(locker, nil, nil, mc, nil).(*ui.StdUIRenderer)
		got := r.GetTimestamp()
		want := "12:34:56"
		if got != want {
			t.Errorf("getTimestamp() = %q, want %q", got, want)
		}
	})

	t.Run("Real clock fallback", func(t *testing.T) {
		r := ui.NewRenderer(locker, nil, nil, nil, nil).(*ui.StdUIRenderer)
		got := r.GetTimestamp()
		// Just verify it doesn't panic and returns a valid looking timestamp (HH:MM:SS)
		if len(got) != 8 || got[2] != ':' || got[5] != ':' {
			t.Errorf("getTimestamp() with real clock returned invalid format: %q", got)
		}
	})
}

func TestStdUIRenderer_NilRendererFallback(t *testing.T) {
	var stdout bytes.Buffer
	_ = ui.NewRenderer(ui.NewMockLocker(), &stdout, &stdout, nil, nil).(*ui.StdUIRenderer)
	// We need to bypass the actual renderer. In external test we can't easily set unexported 'renderer' field to nil
	// unless we export a way to do it.
}

func TestStdUIRenderer_NowSafeRace(t *testing.T) {
	r := ui.NewRenderer(ui.NewMockLocker(), nil, nil, clock.RealClock{}, nil).(*ui.StdUIRenderer)
	stop := make(chan bool)

	// Goroutine 1: Rapidly swap the clock
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				r.SetClock(clock.RealClock{})
				r.SetClock(nil)
			}
		}
	}()

	// Goroutine 2: Rapidly call nowSafe
	for i := 0; i < 1000; i++ {
		r.NowSafe()
	}
	close(stop)
}

func TestStdUIRenderer_LogUsage_Terminal(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	t.Run("LogUsage terminal output for summaries", func(t *testing.T) {
		stderr.Reset()
		metrics := &llm.Metrics{IsSummary: true, PromptTokens: 100, Cost: 0.05}
		r.LogUsage(context.Background(), metrics, t.TempDir()+"/test.log", mc.Now())

		output := stderr.String()
		if !strings.Contains(output, "$0.0500") {
			t.Errorf("expected terminal output to contain cost, got %q", output)
		}
	})
}

func TestStdUIRenderer_ColorLogic(t *testing.T) {
	locker := ui.NewMockLocker()
	tests := []struct {
		name     string
		useColor bool
		input    string
		expected string
	}{
		{"Color Enabled", true, "\033[0;31m", "\033[0;31m"},
		{"Color Disabled", false, "\033[0;31m", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := ui.NewRenderer(locker, nil, nil, clock.RealClock{}, nil).(*ui.StdUIRenderer)
			r.SetUseColor(tt.useColor)
			uiState := r.GetUIState()
			if got := uiState.C(tt.input); got != tt.expected {
				t.Errorf("ui.c(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestStdUIRenderer_Spinner_Cancellation(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	t.Run("StartSpinnerContextCancellation", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		ctx, cancel := context.WithCancel(context.Background())

		r.SetForceSpinner(true)
		stop := r.StartSpinner(ctx)

		cancel() // Cancel the context
		stop()   // Wait for the goroutine to finish

		// If the goroutine leaked, it would still be running, but we can't easily check that without a more complex setup.
		// However, we can check if it cleared the indicator.
		if !strings.Contains(stderr.String(), ui.TermClearLine) {
			t.Errorf("expected stderr to contain clear sequence %q, got %q", ui.TermClearLine, stderr.String())
		}
	})
}

func TestStartSpinner_Synchronization(t *testing.T) {
	combined := testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, combined, combined, mc, nil).(*ui.StdUIRenderer)
	r.SetForceSpinner(true)

	// Start spinner
	stop := r.StartSpinner(context.Background())

	// Call stop
	stop()

	// Immediately write to "stdout" (the same buffer)
	_, _ = fmt.Fprint(combined, "Response")

	output := combined.String()

	// Expected sequence:
	// 1. Spinner frame (\r...)
	// 2. Clear sequence (\r\033[2K)
	// 3. "Response"

	clearIdx := strings.LastIndex(output, ui.TermClearLine)
	respIdx := strings.Index(output, "Response")

	if clearIdx == -1 {
		t.Fatal("clear sequence not found")
	}
	if respIdx == -1 {
		t.Fatal("Response not found")
	}

	if respIdx < clearIdx {
		t.Errorf("Response appeared BEFORE clear sequence: %q", output)
	}
}

type mockSystemMetricsProvider struct {
	Total int64
	Idle  int64
	Mem   float64
}

func (m *mockSystemMetricsProvider) GetCPUStats() (int64, int64) {
	return m.Total, m.Idle
}

func (m *mockSystemMetricsProvider) GetMemoryPercent() float64 {
	return m.Mem
}

func TestStdUIRenderer_SpinnerWithMetrics(t *testing.T) {
	stderr := testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	// Initial stats: total 1000, idle 500 (50% usage)
	mockMetrics := &mockSystemMetricsProvider{
		Total: 1000,
		Idle:  500,
		Mem:   75.0,
	}

	r := ui.NewRenderer(locker, nil, stderr, mc, mockMetrics).(*ui.StdUIRenderer)
	r.SetForceSpinner(true)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Update clock by 1 second and stats
	// Note: total += 1000, idle += 200 => (1- (200/1000)) = 80% usage
	stop := r.StartSpinnerWithMetrics(ctx, "Loading...")

	// Tick clock to trigger first metric recalculation
	mc.Add(time.Second)
	mockMetrics.Total += 1000
	mockMetrics.Idle += 200
	mockMetrics.Mem = 75.0

	// Draw another frame to capture updated metrics
	uiState := r.GetUIState()
	r.DrawLoadingIndicator(uiState, "X", mc.Now().Add(-time.Second), "Loading...", true, mockMetrics)

	stop()

	output := stderr.String()
	// Output should contain: Loading... (1s) [CPU: 80.0% | MEM: 75.0%]
	if !strings.Contains(output, "CPU: 80.0%") {
		t.Errorf("expected output to contain CPU: 80.0%%, got %q", output)
	}
	if !strings.Contains(output, "MEM: 75.0%") {
		t.Errorf("expected output to contain MEM: 75.0%%, got %q", output)
	}
}

func TestDefaultMetricsProvider(t *testing.T) {
	locker := ui.NewMockLocker()
	r := ui.NewRenderer(locker, nil, nil, nil, nil).(*ui.StdUIRenderer)
	mp := r.GetMetricsProvider()

	total, idle := mp.GetCPUStats()
	if total != 0 || idle != 0 {
		t.Errorf("expected 0, 0 from defaultMetricsProvider.GetCPUStats, got %d, %d", total, idle)
	}

	mem := mp.GetMemoryPercent()
	if mem != 0.0 {
		t.Errorf("expected 0.0 from defaultMetricsProvider.GetMemoryPercent, got %f", mem)
	}
}

func TestStdUIRenderer_MarkdownRendering(t *testing.T) {
	var stdout bytes.Buffer
	locker := ui.NewMockLocker()
	r := ui.NewRenderer(locker, &stdout, &stdout, nil, nil).(*ui.StdUIRenderer)

	t.Run("NormalMarkdown", func(t *testing.T) {
		stdout.Reset()
		r.RenderMarkdown("# Hello")
		if !strings.Contains(stdout.String(), "Hello") {
			t.Errorf("expected stdout to contain 'Hello', got %q", stdout.String())
		}
	})

	t.Run("NilRendererFallback", func(t *testing.T) {
		stdout.Reset()
		r.SetGlamourRenderer(nil)
		r.RenderMarkdown("Raw Text")
		if !strings.Contains(stdout.String(), "Raw Text") {
			t.Errorf("expected stdout to contain 'Raw Text', got %q", stdout.String())
		}
	})
}

func TestStdUIRenderer_Setters(t *testing.T) {
	stdout1, stderr1 := &bytes.Buffer{}, &bytes.Buffer{}
	stdout2, stderr2 := &bytes.Buffer{}, &bytes.Buffer{}

	locker := ui.NewMockLocker()
	mc1 := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	mc2 := ui.NewMockClock(time.Date(2027, 1, 1, 12, 0, 0, 0, time.UTC))

	r := ui.NewRenderer(locker, stdout1, stderr1, mc1, nil).(*ui.StdUIRenderer)

	// Initial state
	r.LogSystemMessage(context.Background(), "msg1", "info")
	if !strings.Contains(stderr1.String(), "12:00:00") || !strings.Contains(stderr1.String(), "msg1") {
		t.Errorf("expected stderr1 to contain timestamp and msg1, got %q", stderr1.String())
	}

	// Set new writers and clock
	r.SetWriters(stdout2, stderr2)
	r.SetClock(mc2)

	r.LogSystemMessage(context.Background(), "msg2", "info")
	if !strings.Contains(stderr2.String(), "12:00:00") || !strings.Contains(stderr2.String(), "msg2") {
		// Note: mc2 is set to 2027-01-01 12:00:00, so timestamp should still be 12:00:00 if only time matters,
		// but let's be more specific.
		t.Errorf("expected stderr2 to contain 12:00:00 and msg2, got %q", stderr2.String())
	}

	// Verify stderr1 didn't get msg2
	if strings.Contains(stderr1.String(), "msg2") {
		t.Errorf("expected stderr1 NOT to contain msg2")
	}

	// Test SetClock(nil)
	r.SetClock(nil)
	// It should fallback to real clock, which we can't easily predict but it should not panic.
	r.NowSafe()
}

func TestStdUIRenderer_InlineData(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	content := &llm.Content{
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("xyz")}},
		},
	}
	r.RenderResponse(context.Background(), content, false, false)

	if !strings.Contains(stderr.String(), "[Media] image/png (3 bytes)") {
		t.Errorf("expected stderr to contain media info, got %q", stderr.String())
	}
}

func TestStdUIRenderer_ToolReasons(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	r.LogTurnStatus(context.Background(), events.TurnStatus{
		IsPostCall:  true,
		Metrics:     &llm.Metrics{PromptTokens: 100},
		ToolReasons: []string{"thinking about tool"},
	})

	if !strings.Contains(stderr.String(), "[Tool Reason] thinking about tool") {
		t.Errorf("expected stderr to contain tool reason, got %q", stderr.String())
	}
}

func TestStdUIRenderer_RenderHealthReport(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	tests := []struct {
		name     string
		report   *ports.HealthReport
		contains []string
	}{
		{
			name: "Healthy overall report",
			report: &ports.HealthReport{
				OverallStatus: ports.StatusHealthy,
				Timestamp:     mc.Now(),
				Components: map[ports.Component]ports.ComponentReport{
					ports.CompPersistence: {
						Component: ports.CompPersistence,
						Status:    ports.StatusHealthy,
						Message:   "SQLite database is healthy",
						Details: map[string]any{
							"size_bytes": 1024,
						},
					},
				},
			},
			contains: []string{
				"Overall Status: \033[0;32mHEALTHY",
				"\033[0;32m[HEALTHY]\033[0m persistence  : SQLite database is healthy",
				"\033[0;90msize_bytes:\033[0m 1024",
			},
		},
		{
			name: "Degraded overall report with toolchain binaries",
			report: &ports.HealthReport{
				OverallStatus: ports.StatusDegraded,
				Timestamp:     mc.Now(),
				Components: map[ports.Component]ports.ComponentReport{
					ports.CompToolchain: {
						Component: ports.CompToolchain,
						Status:    ports.StatusDegraded,
						Message:   "Some optional tools are missing",
						Details: map[string]any{
							"binaries": map[string]any{
								"git": map[string]any{
									"version_string": "2.40",
									"is_required":    true,
								},
								"go": map[string]any{
									"version_string": "1.21",
									"is_required":    false,
								},
							},
						},
					},
				},
			},
			contains: []string{
				"Overall Status: \033[0;33mDEGRADED",
				"\033[0;33m[DEGRADED]\033[0m toolchain    : Some optional tools are missing",
				"\033[0;90mgit:\033[0m 2.40 (required)",
				"\033[0;90mgo:\033[0m 1.21",
			},
		},
		{
			name: "Unhealthy overall report",
			report: &ports.HealthReport{
				OverallStatus: ports.StatusUnhealthy,
				Timestamp:     mc.Now(),
				Components: map[ports.Component]ports.ComponentReport{
					ports.CompLLMProvider: {
						Component: ports.CompLLMProvider,
						Status:    ports.StatusUnhealthy,
						Message:   "LLM provider is down",
					},
				},
			},
			contains: []string{
				"Overall Status: \033[0;31mUNHEALTHY",
				"\033[0;31m[UNHEALTHY]\033[0m llm          : LLM provider is down",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr.Reset()
			r.RenderHealthReport(context.Background(), tt.report)
			output := stderr.String()
			for _, s := range tt.contains {
				if !strings.Contains(output, s) {
					t.Errorf("expected output to contain %q, got %q", s, output)
				}
			}
		})
	}
}

func TestRenderMetricsLine_ThinkingSegmentSuppression(t *testing.T) {
	// Pins issue #72: the metrics line must omit the " Th: <n>"
	// segment entirely when ThinkingTokens == 0 (because providers
	// like Anthropic genuinely never count reasoning separately, and
	// "Th: 0" misleads users into thinking no reasoning occurred).
	// When ThinkingTokens > 0, the segment must render as before.
	tests := []struct {
		name           string
		thinkingTokens int32
		shouldContain  string // segment that must appear
		shouldNotHave  string // segment that must NOT appear
	}{
		{
			name:           "zero_thinking_suppressed",
			thinkingTokens: 0,
			shouldContain:  "C: 100", // immediately precedes where Th: would go
			shouldNotHave:  "Th:",
		},
		{
			name:           "nonzero_thinking_rendered",
			thinkingTokens: 147,
			shouldContain:  "Th: 147",
			shouldNotHave:  "", // not used
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
			locker := ui.NewMockLocker()
			mc := ui.NewMockClock(time.Date(2026, 1, 1, 21, 4, 52, 0, time.UTC))
			r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

			r.LogTurnStatus(context.Background(), events.TurnStatus{
				Timestamp:       r.NowSafe(),
				CurrentTurns:    1,
				MaxHistoryTurns: 10,
				IsPostCall:      true,
				Metrics: &llm.Metrics{
					PromptTokens:   500,
					CachedTokens:   0,
					ResponseTokens: 100,
					ThinkingTokens: tt.thinkingTokens,
					Duration:       1.0,
				},
				StartTime: r.NowSafe().Add(-1 * time.Second),
			})

			output := stderr.String()

			if !strings.Contains(output, tt.shouldContain) {
				t.Errorf("expected output to contain %q, got: %q", tt.shouldContain, output)
			}
			if tt.shouldNotHave != "" && strings.Contains(output, tt.shouldNotHave) {
				t.Errorf("expected output to NOT contain %q, got: %q", tt.shouldNotHave, output)
			}
		})
	}
}

// ── Renderer error path hardening tests (Issue #383) ──

func TestStdUIRenderer_IsTerminalContext(t *testing.T) {
	locker := ui.NewMockLocker()

	t.Run("non-os.File stderr returns false", func(t *testing.T) {
		// When stderr is a bytes.Buffer (not *os.File), IsTerminalContext must
		// return false without panicking.
		var buf bytes.Buffer
		r := ui.NewRenderer(locker, &buf, &buf, nil, nil).(*ui.StdUIRenderer)
		if r.IsTerminalContext() {
			t.Error("expected IsTerminalContext to return false for bytes.Buffer stderr")
		}
	})

	t.Run("nil writers return false", func(t *testing.T) {
		r := ui.NewRenderer(locker, nil, nil, nil, nil).(*ui.StdUIRenderer)
		if r.IsTerminalContext() {
			t.Error("expected IsTerminalContext to return false for nil stderr")
		}
	})
}

func TestStdUIRenderer_MarkdownRenderingFallback(t *testing.T) {
	var stdout bytes.Buffer
	locker := ui.NewMockLocker()
	r := ui.NewRenderer(locker, &stdout, &stdout, nil, nil).(*ui.StdUIRenderer)

	t.Run("nil renderer falls back to raw text", func(t *testing.T) {
		stdout.Reset()
		r.SetGlamourRenderer(nil)
		r.RenderMarkdown("**bold**")
		got := stdout.String()
		if !strings.Contains(got, "**bold**") {
			t.Errorf("expected raw markdown text, got %q", got)
		}
	})

	t.Run("non-nil renderer with complex markdown", func(t *testing.T) {
		stdout.Reset()
		r.SetGlamourRenderer(nil) // Reset first
		// Create a new renderer to test the normal path
		r2 := ui.NewRenderer(locker, &stdout, &stdout, nil, nil).(*ui.StdUIRenderer)
		r2.RenderMarkdown("# Heading\n\nBody text")
		got := stdout.String()
		if !strings.Contains(got, "Heading") {
			t.Errorf("expected rendered markdown with Heading, got %q", got)
		}
	})
}

// ── Renderer metrics error path tests (Issue #383) ──

func TestStdUIRenderer_LogUsage_NilAndEmpty(t *testing.T) {
	stdout, stderr := testfixtures.NewSafeBuffer(), testfixtures.NewSafeBuffer()
	locker := ui.NewMockLocker()
	mc := ui.NewMockClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	r := ui.NewRenderer(locker, stdout, stderr, mc, nil).(*ui.StdUIRenderer)

	t.Run("nil metrics returns early", func(t *testing.T) {
		stderr.Reset()
		r.LogUsage(context.Background(), nil, t.TempDir()+"/test.log", mc.Now())
		// Should not panic and should not write to stderr (nil metrics)
	})

	t.Run("empty logFile returns early", func(t *testing.T) {
		stderr.Reset()
		m := &llm.Metrics{PromptTokens: 100}
		r.LogUsage(context.Background(), m, "", mc.Now())
		// Should not panic
	})

	t.Run("logFile with unwritable path returns early", func(t *testing.T) {
		stderr.Reset()
		m := &llm.Metrics{PromptTokens: 100}
		// Use a path that can't be written to (directory that doesn't exist)
		r.LogUsage(context.Background(), m, "/nonexistent/dir/log.json", mc.Now())
		// Should not panic — error from os.OpenFile is silently ignored
	})
}
