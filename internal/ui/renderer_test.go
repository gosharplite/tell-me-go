// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

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
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// safeBuffer is a thread-safe wrapper around bytes.Buffer for testing concurrent I/O.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *safeBuffer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b.Reset()
}

func (s *safeBuffer) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Len()
}

func TestStdUIRenderer_BasicLogging(t *testing.T) {
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

	t.Run("LogSystemMessage", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		r.LogSystemMessage("test message", "error")
		if !strings.Contains(stderr.String(), "test message") {
			t.Errorf("expected stderr to contain 'test message', got %q", stderr.String())
		}
	})

	t.Run("LogTurnStatus", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(events.TurnStatus{
			Timestamp:        mc.Now(),
			CurrentTurns:     0,
			SessionTurns:     0,
			MaxHistoryTurns:  10,
			Tokens:           100,
			MaxHistoryTokens: 1000,
			Mode:             "coder",
		})
		output := stderr.String()
		if !strings.Contains(output, "Turn 1/10 - Coder") {
			t.Errorf("expected stderr to contain 'Turn 1/10 - Coder', got %q", output)
		}
		// Payload line: contains tokens/max, and - Coder.
		// Note: contains ~ since it's not a post-call
		if !strings.Contains(output, "Payload:") || !strings.Contains(output, "/1000 tokens - Coder") {
			t.Errorf("expected stderr to contain 'Payload: ... /1000 tokens - Coder', got %q", output)
		}
		// Check for the trailing newline (visual gap)
		if !strings.HasSuffix(output, "\n\n") {
			t.Errorf("expected stderr to end with double newline for visual gap, got %q", output)
		}
	})

	t.Run("LogTurnStatus_NoMaxHistoryTurns", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(events.TurnStatus{
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
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

	t.Run("LogTurnStatus_PostCall", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(events.TurnStatus{
			Timestamp:       r.nowSafe(),
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
			StartTime: r.nowSafe().Add(-5 * time.Second),
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
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

	t.Run("LogToolCall_WithShowTools", func(t *testing.T) {
		stderr.Reset()
		r.LogToolCall([]*llm.FunctionCall{{Name: "my_tool", Args: map[string]interface{}{"key": "val", "reason": "my intent"}}}, 0, 5, true)
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
		r.LogToolResult("my_tool", tools.ToolResult{Text: "output", BinaryData: []tools.BinaryData{{MIMEType: "image/png", Data: []byte("xyz")}}}, true)
		if !strings.Contains(stderr.String(), "Tool Result") || !strings.Contains(stderr.String(), "image/png") {
			t.Errorf("expected stderr to contain 'Tool Result' and 'image/png', got %q", stderr.String())
		}
	})
}

func TestStdUIRenderer_ResponseRendering(t *testing.T) {
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

	t.Run("RenderResponse_Markdown", func(t *testing.T) {
		stdout.Reset()
		content := &llm.Content{Parts: []*llm.Part{{Text: "# Title\nbody"}}}
		r.RenderResponse(content, false, false)
		if !strings.Contains(stdout.String(), "Title") {
			t.Errorf("expected stdout to contain 'Title', got %q", stdout.String())
		}
	})

	t.Run("RenderResponse_Thoughts", func(t *testing.T) {
		stderr.Reset()
		content := &llm.Content{Parts: []*llm.Part{{Text: "I am thinking", IsThought: true}}}
		r.RenderResponse(content, true, false)
		if !strings.Contains(stderr.String(), "Thinking") || !strings.Contains(stderr.String(), "I am thinking") {
			t.Errorf("expected stderr to contain 'Thinking', got %q", stderr.String())
		}
	})
}

func TestStdUIRenderer_Spinner(t *testing.T) {
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

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
		ui := r.getUIState()
		stdout.Reset()
		stderr.Reset()
		r.drawLoadingIndicator(ui, "X", mc.now, " Thinking...")
		if !strings.Contains(stderr.String(), "X Thinking...") {
			t.Errorf("expected stderr to contain spinner, got %q", stderr.String())
		}
		if stdout.Len() > 0 {
			t.Errorf("expected stdout to be empty, got %q", stdout.String())
		}
	})

	t.Run("clearLoadingIndicator outputs to stderr", func(t *testing.T) {
		ui := r.getUIState()
		stdout.Reset()
		stderr.Reset()
		r.clearLoadingIndicator(ui, false)
		if stderr.Len() == 0 {
			t.Error("expected stderr to contain clear sequence")
		}
		if stdout.Len() > 0 {
			t.Errorf("expected stdout to be empty, got %q", stdout.String())
		}
	})
}

func TestLogTurnStatus_Format(t *testing.T) {
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 21, 4, 52, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

	r.LogTurnStatus(events.TurnStatus{
		Timestamp:       r.nowSafe(),
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
		StartTime: r.nowSafe().Add(-8330 * time.Millisecond), // 8.33s total
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

	t.Run("CumulativeToolDuration", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(events.TurnStatus{
			Timestamp:  r.nowSafe(),
			IsPostCall: true,
			Metrics: &llm.Metrics{
				Duration:               5.0,
				ToolDuration:           2.0,
				CumulativeToolDuration: 3.5,
			},
			StartTime: r.nowSafe().Add(-10 * time.Second),
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
	r.LogTurnStatus(events.TurnStatus{
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
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

	t.Run("Green cost in LogTurnStatus", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(events.TurnStatus{
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
		r.LogTurnStatus(events.TurnStatus{
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
		r.LogTurnStatus(events.TurnStatus{
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
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

	t.Run("Tool metrics omit total duration", func(t *testing.T) {
		stderr.Reset()
		m := &llm.Metrics{
			PromptTokens:   100,
			ResponseTokens: 50,
			Duration:       1.5,
		}
		r.LogToolResult("test_tool", tools.ToolResult{
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
		r.LogTurnStatus(events.TurnStatus{
			IsPostCall: true,
			StartTime:  r.nowSafe().Add(-5 * time.Second),
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
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	r := NewRenderer(locker, &stdout, &stderr, clock.RealClock{}).(*stdUIRenderer)

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
				r.RenderResponse(content, false, true)

				// Test LogSystemMessage
				r.LogSystemMessage(fmt.Sprintf("G%d-I%d-Sys", id, j), "info")
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
	locker := &mockLocker{}

	t.Run("Mocked time", func(t *testing.T) {
		mc := &mockClock{now: time.Date(2026, 1, 1, 12, 34, 56, 0, time.UTC)}
		r := &stdUIRenderer{
			locker: locker,
			clock:  mc,
		}
		got := r.getTimestamp()
		want := "12:34:56"
		if got != want {
			t.Errorf("getTimestamp() = %q, want %q", got, want)
		}
	})

	t.Run("Real clock fallback", func(t *testing.T) {
		r := NewRenderer(locker, nil, nil, nil).(*stdUIRenderer)
		got := r.getTimestamp()
		// Just verify it doesn't panic and returns a valid looking timestamp (HH:MM:SS)
		if len(got) != 8 || got[2] != ':' || got[5] != ':' {
			t.Errorf("getTimestamp() with real clock returned invalid format: %q", got)
		}
	})
}

func TestStdUIRenderer_NilRendererFallback(t *testing.T) {
	var stdout bytes.Buffer
	r := &stdUIRenderer{
		stdout:   &stdout,
		renderer: nil, // Explicitly nil
		clock:    clock.RealClock{},
	}

	testText := "# Hello World"
	r.renderMarkdown(testText)

	output := stdout.String()
	if !strings.Contains(output, testText) {
		t.Errorf("Expected raw text output when renderer is nil, got: %q", output)
	}
}

func TestStdUIRenderer_NowSafeRace(t *testing.T) {
	r := &stdUIRenderer{
		clock: clock.RealClock{},
	}
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
		r.nowSafe()
	}
	close(stop)
}

func TestStdUIRenderer_LogUsage_Terminal(t *testing.T) {
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

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
	locker := &mockLocker{}
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
			r := NewRenderer(locker, nil, nil, clock.RealClock{}).(*stdUIRenderer)
			r.SetUseColor(tt.useColor)
			ui := r.getUIState()
			if got := ui.c(tt.input); got != tt.expected {
				t.Errorf("ui.c(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestStdUIRenderer_Spinner_Cancellation(t *testing.T) {
	var stdout, stderr safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

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
		if !strings.Contains(stderr.String(), termClearLine) {
			t.Errorf("expected stderr to contain clear sequence %q, got %q", termClearLine, stderr.String())
		}
	})
}

func TestStartSpinner_Synchronization(t *testing.T) {
	var combined safeBuffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &combined, &combined, mc).(*stdUIRenderer)
	r.SetForceSpinner(true)

	// Start spinner
	stop := r.StartSpinner(context.Background())

	// Call stop
	stop()

	// Immediately write to "stdout" (the same buffer)
	_, _ = fmt.Fprint(&combined, "Response")

	output := combined.String()

	// Expected sequence:
	// 1. Spinner frame (\r...)
	// 2. Clear sequence (\r\033[2K)
	// 3. "Response"

	clearIdx := strings.LastIndex(output, termClearLine)
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
