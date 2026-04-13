// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

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
