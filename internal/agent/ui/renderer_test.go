// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestCalculateVisualLines(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  int
	}{
		{"empty", "", 80, 0},
		{"short", "hello", 80, 1},
		{"exact wrap", "12345", 5, 1},
		{"wrap over", "123456", 5, 2},
		{"newlines", "a\nb\nc", 80, 3},
		{"zero width fallback", "abc", 0, 1},
	}
	r := &StdUIRenderer{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.calculateVisualLines(tt.text, tt.width); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func FuzzCalculateVisualLines(f *testing.F) {
	f.Add("standard text sample", 80)
	f.Fuzz(func(t *testing.T, text string, width int) {
		r := &StdUIRenderer{}
		// Ensure it never panics regardless of input or width
		_ = r.calculateVisualLines(text, width)
	})
}

func TestStdUIRenderer_BasicLogging(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)
	r.now = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }

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
			MaxHistoryTurns:  10,
			Tokens:           100,
			MaxHistoryTokens: 1000,
		})
		if !strings.Contains(stderr.String(), "Turn 1/10") {
			t.Errorf("expected stderr to contain 'Turn 1/10', got %q", stderr.String())
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
		r.LogUsage(metrics, tmpFile, r.now())

		data, err := os.ReadFile(tmpFile)
		if err != nil {
			t.Fatalf("failed to read usage log: %v", err)
		}
		if !strings.Contains(string(data), "T: 15") {
			t.Errorf("expected usage log to contain 'T: 15', got %q", string(data))
		}
	})
}

func TestStdUIRenderer_AdvancedLogging(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sm := security.NewSecurityManager(nil)
	r := NewStdUIRenderer(sm)
	r.SetWriters(&stdout, &stderr)
	r.now = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }

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
		if !strings.Contains(stderr.String(), "Ready") {
			t.Errorf("expected stderr to contain 'Ready', got %q", stderr.String())
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
	r.now = func() time.Time { return time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) }

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
}
