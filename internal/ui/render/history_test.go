// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package render

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/ui/colors"
)

func TestHistory_Rendering(t *testing.T) {
	ctx := context.Background()
	h := history.NewManager("")
	h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "hello"}}})
	h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{
		{Thought: true, Text: "I am thinking"},
		{Text: "hi there"},
	}})

	t.Run("HideThoughts", func(t *testing.T) {
		var buf bytes.Buffer
		History(&buf, h, 10, RenderOptions{Raw: true, ShowThoughts: false})
		
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
		History(&buf, h, 10, RenderOptions{Raw: true, ShowThoughts: true})
		
		output := buf.String()
		if !strings.Contains(output, "I am thinking") {
			t.Errorf("output should contain thoughts")
		}
	})

	t.Run("UseColor", func(t *testing.T) {
		var buf bytes.Buffer
		History(&buf, h, 10, RenderOptions{Raw: true, UseColor: true})
		
		output := buf.String()
		if !strings.Contains(output, colors.ColorBlue) {
			t.Errorf("output should contain color codes for user role")
		}
	})
}


func TestHistory_Empty(t *testing.T) {
	h := history.NewManager("")
	var buf bytes.Buffer
	History(&buf, h, 10, RenderOptions{Raw: true})
	
	if !strings.Contains(buf.String(), "No history found.") {
		t.Errorf("expected 'No history found.', got %q", buf.String())
	}
}

func TestHistory_RenderPart_Tool(t *testing.T) {
	ctx := context.Background()
	h := history.NewManager("")
	h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "call tool"}}})
	h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{
		{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
	}})
	h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{
		{FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "result"}}},
	}})

	var buf bytes.Buffer
	History(&buf, h, 10, RenderOptions{Raw: true})
	
	output := buf.String()
	if !strings.Contains(output, "[Tool Call] test_tool") {
		t.Errorf("output should contain tool call")
	}
	if !strings.Contains(output, "[Tool Response] test_tool") {
		t.Errorf("output should contain tool response")
	}
}
