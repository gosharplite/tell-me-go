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
	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestStdUIRenderer_BasicLogging(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)
	r.SetNow(func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) })

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
			Timestamp:        r.now(),
			CurrentTurns:     0,
			SessionTurns:     0,
			MaxHistoryTurns:  10,
			Tokens:           100,
			MaxHistoryTokens: 1000,
		})
		output := stderr.String()
		if !strings.Contains(output, "Session: 1/10 turns") {
			t.Errorf("expected stderr to contain 'Session: 1/10 turns', got %q", output)
		}
		// Check for the trailing newline (visual gap)
		if !strings.HasSuffix(output, "\n\n") {
			t.Errorf("expected stderr to end with double newline for visual gap, got %q", output)
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
		r.LogUsage(context.Background(), metrics, tmpFile, r.now())

		data, err := os.ReadFile(tmpFile)
		if err != nil {
			t.Fatalf("failed to read usage log: %v", err)
		}
		if !strings.Contains(string(data), "\"total_tokens\":15") {
			t.Errorf("expected usage log to contain '\"total_tokens\":15', got %q", string(data))
		}
	})
}

func TestStdUIRenderer_AdvancedLogging(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)
	r.SetNow(func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) })

	t.Run("LogTurnStatus_PostCall", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(events.TurnStatus{
			Timestamp:       r.now(),
			CurrentTurns:    1,
			MaxHistoryTurns: 10,
			IsPostCall:      true,
			Metrics: &llm.Metrics{
				PromptTokens:   500,
				CachedTokens:   200,
				ResponseTokens: 100,
				TotalTokens:    600,
				Duration:       2.0,
			},
			StartTime: r.now().Add(-5 * time.Second),
		})
		output := stderr.String()
		if !strings.HasPrefix(output, "\n") {
			t.Errorf("expected stderr to start with a newline, got %q", output)
		}
		if !strings.Contains(output, "Ready") {
			t.Errorf("expected stderr to contain 'Ready', got %q", output)
		}
	})

	t.Run("LogToolCall_WithShowTools", func(t *testing.T) {
		stderr.Reset()
		r.LogToolCall([]*llm.FunctionCall{{Name: "my_tool", Args: map[string]interface{}{"key": "val"}}}, 0, 5, true)
		if !strings.Contains(stderr.String(), "Tool Action") || !strings.Contains(stderr.String(), "my_tool") {
			t.Errorf("expected stderr to contain 'Tool Action' and 'my_tool', got %q", stderr.String())
		}
	})

	t.Run("LogToolResult_WithShowTools", func(t *testing.T) {
		stderr.Reset()
		r.LogToolResult("my_tool", tools.ToolResult{Text: "output", BinaryData: []tools.BinaryData{{MIMEType: "image/png", Data: []byte("xyz")}}}, true)
		if !strings.Contains(stderr.String(), "Tool Result") || !strings.Contains(stderr.String(), "image/png") {
			t.Errorf("expected stderr to contain 'Tool Result' and 'image/png', got %q", stderr.String())
		}
	})

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
		content := &llm.Content{Parts: []*llm.Part{{Text: "I am thinking", Thought: true}}}
		r.RenderResponse(content, true, false)
		if !strings.Contains(stderr.String(), "Thinking") || !strings.Contains(stderr.String(), "I am thinking") {
			t.Errorf("expected stderr to contain 'Thinking', got %q", stderr.String())
		}
	})
}

func TestStdUIRenderer_Streaming(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)
	r.SetNow(func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) })

	t.Run("StreamResponse_Simple", func(t *testing.T) {
		stdout.Reset()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, finalize := r.StreamResponse(ctx, false, true) // rawOutput=true for simplicity
		ch <- &llm.Content{Parts: []*llm.Part{{Text: "Hello"}}}
		ch <- &llm.Content{Parts: []*llm.Part{{Text: " World"}}}

		agg := finalize()
		var aggText string
		for _, p := range agg.Parts {
			aggText += p.Text
		}
		if aggText != "Hello World" {
			t.Errorf("expected aggregated text 'Hello World', got %q", aggText)
		}
		if stdout.String() != "Hello World" {
			t.Errorf("expected stdout 'Hello World', got %q", stdout.String())
		}
	})

	t.Run("StreamResponse_WithThoughts", func(t *testing.T) {
		stderr.Reset()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, finalize := r.StreamResponse(ctx, true, true)
		ch <- &llm.Content{Parts: []*llm.Part{{Text: "Thinking...", Thought: true}}}
		ch <- &llm.Content{Parts: []*llm.Part{{Text: "Result"}}}

		_ = finalize()
		if !strings.Contains(stderr.String(), "Thinking...") {
			t.Errorf("expected stderr to contain 'Thinking...', got %q", stderr.String())
		}
	})

	t.Run("StreamResponse_WithMedia", func(t *testing.T) {
		stderr.Reset()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, finalize := r.StreamResponse(ctx, false, true)
		ch <- &llm.Content{Parts: []*llm.Part{{
			InlineData: &llm.Blob{
				MIMEType: "image/png",
				Data:     []byte("fake-image-data"),
			},
		}}}

		_ = finalize()
		output := stderr.String()
		if !strings.Contains(output, "[Media]") || !strings.Contains(output, "image/png") {
			t.Errorf("expected stderr to contain '[Media]' and 'image/png', got %q", output)
		}
	})
}

func TestLogTurnStatus_Format(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)
	r.SetNow(func() time.Time { return time.Date(2026, 1, 1, 21, 4, 52, 0, time.UTC) })

	r.LogTurnStatus(events.TurnStatus{
		Timestamp:       r.now(),
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
		StartTime: r.now().Add(-8330 * time.Millisecond), // 8.33s total
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
		"/ 8.33s",
	}

	for _, p := range parts {
		if !strings.Contains(output, p) {
			t.Errorf("expected output to contain %q, got %q", p, output)
		}
	}

	// Check Ready line with aggregates
	r.LogTurnStatus(events.TurnStatus{
		Timestamp:   r.now(),
		IsPostCall:  true,
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

func TestStreamResponseCursorAnchoring(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)

	t.Run("Anchoring enabled when rawOutput is false", func(t *testing.T) {
		stdout.Reset()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, finalize := r.StreamResponse(ctx, false, false)
		ch <- &llm.Content{Parts: []*llm.Part{{Text: "Streaming chunk"}}}
		_ = finalize()

		output := stdout.String()
		// Should contain Save Cursor
		if !strings.Contains(output, "\0337") {
			t.Errorf("Expected output to contain DEC Save Cursor (\\0337), got %q", output)
		}
		// Should contain Restore Cursor
		if !strings.Contains(output, "\0338") {
			t.Errorf("Expected output to contain DEC Restore Cursor (\\0338), got %q", output)
		}
		// Should contain Clear to End of Screen
		if !strings.Contains(output, "\033[J") {
			t.Errorf("Expected output to contain Clear to End of Screen (\\033[J), got %q", output)
		}
	})

	t.Run("Anchoring disabled when rawOutput is true", func(t *testing.T) {
		stdout.Reset()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ch, finalize := r.StreamResponse(ctx, false, true)
		ch <- &llm.Content{Parts: []*llm.Part{{Text: "Streaming chunk"}}}
		_ = finalize()

		output := stdout.String()
		if strings.Contains(output, "\0337") {
			t.Errorf("Expected output NOT to contain DEC Save Cursor, got %q", output)
		}
		if strings.Contains(output, "\0338") {
			t.Errorf("Expected output NOT to contain DEC Restore Cursor, got %q", output)
		}
	})
}

func TestStdUIRenderer_Colors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)
	r.SetNow(func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) })

	t.Run("Green cost in LogTurnStatus", func(t *testing.T) {
		stderr.Reset()
		r.LogTurnStatus(events.TurnStatus{
			IsPostCall:   true,
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
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)
	r.SetNow(func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) })

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
			StartTime:  r.now().Add(-5 * time.Second),
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
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)

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
				// Test RenderResponse (which now locks around both parts)
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
	sm := security.NewSecurityManager(nil)

	t.Run("Mocked time", func(t *testing.T) {
		r := &StdUIRenderer{
			sm: sm,
		}
		r.SetNow(func() time.Time { return time.Date(2026, 1, 1, 12, 34, 56, 0, time.UTC) })
		got := r.getTimestamp()
		want := "12:34:56"
		if got != want {
			t.Errorf("getTimestamp() = %q, want %q", got, want)
		}
	})

	t.Run("Nil now fallback", func(t *testing.T) {
		r := &StdUIRenderer{sm: sm}
		got := r.getTimestamp()
		// Just verify it doesn't panic and returns a valid looking timestamp (HH:MM:SS)
		if len(got) != 8 || got[2] != ':' || got[5] != ':' {
			t.Errorf("getTimestamp() with nil now returned invalid format: %q", got)
		}
	})
}

func TestStdUIRenderer_NilRendererFallback(t *testing.T) {
	var stdout bytes.Buffer
	r := &StdUIRenderer{
		stdout:   &stdout,
		renderer: nil, // Explicitly nil
	}

	testText := "# Hello World"
	r.renderMarkdown(testText)

	output := stdout.String()
	if !strings.Contains(output, testText) {
		t.Errorf("Expected raw text output when renderer is nil, got: %q", output)
	}
}

func TestStdUIRenderer_NowSafeRace(t *testing.T) {
	r := &StdUIRenderer{}
	stop := make(chan bool)

	// Goroutine 1: Rapidly swap the 'now' function
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				r.SetNow(func() time.Time { return time.Now() })
				r.SetNow(nil)
			}
		}
	}()

	// Goroutine 2: Rapidly call nowSafe
	for i := 0; i < 1000; i++ {
		r.nowSafe()
	}
	close(stop)
}

func TestStreamResponse_ScrollAware(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)

	t.Run("Redraw on no scroll", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		ctx := context.Background()
		ch, finalize := r.StreamResponse(ctx, false, false)
		ch <- &llm.Content{Parts: []*llm.Part{{Text: "Small response"}}}
		_ = finalize()

		// Should contain Restore Cursor
		if !strings.Contains(stdout.String(), "\0338") {
			t.Errorf("expected restore cursor code, got %q", stdout.String())
		}
		if strings.Contains(stderr.String(), "── (formatted) ──") {
			t.Errorf("did not expect scroll separator, got %q", stderr.String())
		}
	})

	t.Run("Failover on scroll", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		ctx := context.Background()
		ch, finalize := r.StreamResponse(ctx, false, false)
		
		// Send 30 newlines to trigger hasScrolled
		longText := strings.Repeat("line\n", 30)
		ch <- &llm.Content{Parts: []*llm.Part{{Text: longText}}}
		_ = finalize()

		// Should NOT contain Restore Cursor in the finalization phase (it might be there from the start of stream, but let's check the separator)
		if !strings.Contains(stderr.String(), "── (formatted) ──") {
			t.Errorf("expected scroll separator, got %q", stderr.String())
		}
	})

	t.Run("Long response scroll detection", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		
		// Set a low threshold for scrolling simulation if possible, 
		// but since it's hardcoded to 25, we just test that it works at that threshold.
		// The instruction was: "simulates a 'narrow' terminal to verify behavior."
		// Since the renderer doesn't actually query terminal size yet, 
		// "narrow" in this context means many small increments that hit the line count.
		
		ctx := context.Background()
		ch, finalize := r.StreamResponse(ctx, false, false)
		
		// Simulate many small chunks each containing a newline
		for i := 0; i < 26; i++ {
			ch <- &llm.Content{Parts: []*llm.Part{{Text: "chunk\n"}}}
		}
		
		_ = finalize()
		
		if !strings.Contains(stderr.String(), "── (formatted) ──") {
			t.Errorf("expected scroll separator after 26 lines, got %q", stderr.String())
		}
	})
}
