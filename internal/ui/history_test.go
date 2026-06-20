// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

func TestHistory_Rendering(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	historyPath := filepath.Join(tmp, "history.json")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	if err := h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}); err != nil {
		t.Fatal(err)
	}
	if err := h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{
		{IsThought: true, Text: "I am thinking"},
		{Text: "hi there"},
	}}); err != nil {
		t.Fatal(err)
	}

	t.Run("HideThoughts", func(t *testing.T) {
		var buf bytes.Buffer
		ui.RenderHistory(&buf, h, 10, ports.HistoryRenderOptions{Raw: true, ShowThoughts: false})

		output := buf.String()
		if strings.Contains(output, "I am thinking") {
			t.Errorf("output should not contain thoughts")
		}
		if !strings.Contains(output, "hi there") {
			t.Errorf("output should contain response text")
		}
	})

	t.Run("ShowThoughts", func(t *testing.T) {
		var buf bytes.Buffer
		ui.RenderHistory(&buf, h, 10, ports.HistoryRenderOptions{Raw: true, ShowThoughts: true})

		output := buf.String()
		if !strings.Contains(output, "I am thinking") {
			t.Errorf("output should contain thoughts")
		}
	})

	t.Run("UseColor", func(t *testing.T) {
		var buf bytes.Buffer
		ui.RenderHistory(&buf, h, 10, ports.HistoryRenderOptions{Raw: true, UseColor: true})

		output := buf.String()
		if !strings.Contains(output, ui.ColorBlue) {
			t.Errorf("output should contain color codes for user role")
		}
	})
}

func TestHistory_Empty(t *testing.T) {
	tmp := t.TempDir()
	historyPath := filepath.Join(tmp, "history.json")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	var buf bytes.Buffer
	ui.RenderHistory(&buf, h, 10, ports.HistoryRenderOptions{Raw: true})

	if !strings.Contains(buf.String(), "No history found.") {
		t.Errorf("expected 'No history found.', got %q", buf.String())
	}
}

func TestHistory_RenderPart_Tool(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	historyPath := filepath.Join(tmp, "history.json")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	if err := h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "call tool"}}}); err != nil {
		t.Fatal(err)
	}
	if err := h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{
		{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{
		{FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "result"}}},
	}}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	ui.RenderHistory(&buf, h, 10, ports.HistoryRenderOptions{Raw: true})

	output := buf.String()
	if !strings.Contains(output, "[Tool Call] test_tool") {
		t.Errorf("output should contain tool call")
	}
	if !strings.Contains(output, "[Tool Response] test_tool") {
		t.Errorf("output should contain tool response")
	}
}

func TestStdHistoryRenderer_Render(t *testing.T) {
	tmp := t.TempDir()
	historyPath := filepath.Join(tmp, "history.json")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")

	var r ui.StdHistoryRenderer
	var buf bytes.Buffer
	r.Render(&buf, h, 10, ports.HistoryRenderOptions{Raw: true})

	if !strings.Contains(buf.String(), "No history found.") {
		t.Errorf("expected 'No history found.', got %q", buf.String())
	}
}

func TestHistory_NonRaw(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	historyPath := filepath.Join(tmp, "history.json")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	if err := h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "# Hello"}}}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	ui.RenderHistory(&buf, h, 10, ports.HistoryRenderOptions{Raw: false})

	output := buf.String()
	if output == "" {
		t.Errorf("expected some output for non-raw rendering")
	}
}

// ── History error path hardening tests (Issue #383) ──

// mockErrorHistoryReader implements ports.HistoryManager and returns errors from
// GetWindow for testing error rendering paths.
type mockErrorHistoryReader struct {
	totalEntries int
	getWindowErr error
}

func (m *mockErrorHistoryReader) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	return nil, m.getWindowErr
}

func (m *mockErrorHistoryReader) GetTotalEntries() int {
	return m.totalEntries
}

func (m *mockErrorHistoryReader) GetLastUserMessage(ctx context.Context) (string, int, error) {
	return "", 0, nil
}

func (m *mockErrorHistoryReader) GetResolver() llm.AssetResolver { return nil }

// HistoryWriter stubs
func (m *mockErrorHistoryReader) SetContents(ctx context.Context, contents []*llm.Content) error {
	return nil
}
func (m *mockErrorHistoryReader) AddContent(ctx context.Context, content *llm.Content) error {
	return nil
}
func (m *mockErrorHistoryReader) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	return nil
}
func (m *mockErrorHistoryReader) Save(ctx context.Context) error { return nil }
func (m *mockErrorHistoryReader) Sync(ctx context.Context) error { return nil }

// HistoryModifier stubs
func (m *mockErrorHistoryReader) Archive(ctx context.Context, contents []*llm.Content) error {
	return nil
}
func (m *mockErrorHistoryReader) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	return nil
}
func (m *mockErrorHistoryReader) GetFilePath() string { return "" }
func (m *mockErrorHistoryReader) RollbackTurns(ctx context.Context, turns int) (int, int, int, error) {
	return 0, 0, 0, nil
}

func TestHistory_GetWindowError(t *testing.T) {
	t.Run("GetWindow error renders formatted error message", func(t *testing.T) {
		mock := &mockErrorHistoryReader{
			totalEntries: 10,
			getWindowErr: errors.New("database connection lost"),
		}
		var buf bytes.Buffer
		ui.RenderHistory(&buf, mock, 5, ports.HistoryRenderOptions{Raw: true})

		output := buf.String()
		if !strings.Contains(output, "Error retrieving history: database connection lost") {
			t.Errorf("expected error message in output, got %q", output)
		}
	})

	t.Run("GetWindow error with UseColor enabled", func(t *testing.T) {
		mock := &mockErrorHistoryReader{
			totalEntries: 5,
			getWindowErr: errors.New("permission denied"),
		}
		var buf bytes.Buffer
		ui.RenderHistory(&buf, mock, 5, ports.HistoryRenderOptions{Raw: true, UseColor: true})

		output := buf.String()
		if !strings.Contains(output, "Error retrieving history: permission denied") {
			t.Errorf("expected error message in output with color, got %q", output)
		}
	})
}

func TestHistory_RenderTextFallback(t *testing.T) {
	// Test the raw path and non-raw (renderer) path for renderText
	tmp := t.TempDir()
	historyPath := filepath.Join(tmp, "history.json")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")

	ctx := context.Background()
	if err := h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "hello world"}}}); err != nil {
		t.Fatal(err)
	}

	t.Run("raw=true bypasses glamour renderer", func(t *testing.T) {
		var buf bytes.Buffer
		ui.RenderHistory(&buf, h, 10, ports.HistoryRenderOptions{Raw: true})
		output := buf.String()
		// Raw path should contain the text as-is
		if !strings.Contains(output, "hello world") {
			t.Errorf("expected raw text 'hello world', got %q", output)
		}
	})

	t.Run("raw=false uses glamour renderer", func(t *testing.T) {
		var buf bytes.Buffer
		ui.RenderHistory(&buf, h, 10, ports.HistoryRenderOptions{Raw: false})
		output := stripANSI(buf.String())
		// Non-raw path should still contain the content (rendered by glamour)
		// ANSI escapes are stripped first — glamour may emit color codes
		// depending on terminal environment state set by prior tests.
		if !strings.Contains(output, "hello world") {
			t.Errorf("expected rendered text to contain 'hello world', got %q", output)
		}
	})
}

// ── History render error reporting tests (G20) ──

func TestHistory_RenderTextError(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	historyPath := filepath.Join(tmp, "history.json")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
	if err := h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "hello world"}}}); err != nil {
		t.Fatal(err)
	}

	t.Run("non-raw with nil renderer falls back to raw text", func(t *testing.T) {
		var buf bytes.Buffer
		ui.RenderHistory(&buf, h, 10, ports.HistoryRenderOptions{Raw: false})
		output := stripANSI(buf.String())
		if !strings.Contains(output, "hello world") {
			t.Errorf("expected fallback text 'hello world', got: %q", output)
		}
	})

	t.Run("renderer error prints [render error:] prefix and raw text", func(t *testing.T) {
		// Use a fresh history with one entry
		ctx := context.Background()
		tmp := t.TempDir()
		historyPath := filepath.Join(tmp, "history.json")
		h := history.NewManager(infrapersistence.NewOSFileSystem(), historyPath, historyPath+".archive")
		if err := h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "hello world"}}}); err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		ui.RenderHistory(&buf, h, 10, ports.HistoryRenderOptions{
			Raw:            false,
			CustomRenderer: &failingRenderer{err: errors.New("history render failure")},
		})

		output := buf.String()
		// Must contain the [render error: ...] prefix from the degradation path
		if !strings.Contains(output, "[render error: history render failure]") {
			t.Errorf("expected [render error: history render failure] in output, got: %q", output)
		}
		// Must still contain the raw text after the error line
		if !strings.Contains(output, "hello world") {
			t.Errorf("expected raw text 'hello world' after error, got: %q", output)
		}
	})
}
