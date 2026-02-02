// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"reflect"
	"testing"
)

func TestContent_AddPart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		initial  *Content
		newPart  *Part
		wantLen  int
		wantText string
		check    func(t *testing.T, c *Content)
	}{
		{
			name:    "nil part",
			initial: &Content{Parts: []*Part{{Text: "initial"}}},
			newPart: nil,
			wantLen: 1,
			check: func(t *testing.T, c *Content) {
				if c.Parts[0].Text != "initial" {
					t.Errorf("expected 'initial', got %q", c.Parts[0].Text)
				}
			},
		},
		{
			name:     "merge text to empty",
			initial:  &Content{},
			newPart:  &Part{Text: "hello"},
			wantLen:  1,
			wantText: "hello",
		},
		{
			name:     "merge text to text",
			initial:  &Content{Parts: []*Part{{Text: "hello "}}},
			newPart:  &Part{Text: "world"},
			wantLen:  1,
			wantText: "hello world",
		},
		{
			name:    "don't merge text to thought",
			initial: &Content{Parts: []*Part{{Text: "thinking", Thought: true}}},
			newPart: &Part{Text: "answer"},
			wantLen: 2,
			check: func(t *testing.T, c *Content) {
				if c.Parts[0].Text != "thinking" || !c.Parts[0].Thought {
					t.Error("first part mismatch")
				}
				if c.Parts[1].Text != "answer" || c.Parts[1].Thought {
					t.Error("second part mismatch")
				}
			},
		},
		{
			name:    "merge thought to thought",
			initial: &Content{Parts: []*Part{{Text: "think 1", Thought: true}}},
			newPart: &Part{Text: " think 2", Thought: true},
			wantLen: 1,
			check: func(t *testing.T, c *Content) {
				if c.Parts[0].Text != "think 1 think 2" || !c.Parts[0].Thought {
					t.Errorf("expected merged thought 'think 1 think 2', got %q", c.Parts[0].Text)
				}
			},
		},
		{
			name:    "don't merge function call",
			initial: &Content{Parts: []*Part{{Text: "hello"}}},
			newPart: &Part{FunctionCall: &FunctionCall{Name: "test"}},
			wantLen: 2,
			check: func(t *testing.T, c *Content) {
				if c.Parts[1].FunctionCall == nil || c.Parts[1].FunctionCall.Name != "test" {
					t.Error("function call not appended correctly")
				}
			},
		},
		{
			name:    "don't merge function response",
			initial: &Content{Parts: []*Part{{Text: "hello"}}},
			newPart: &Part{FunctionResponse: &FunctionResponse{Name: "test"}},
			wantLen: 2,
		},
		{
			name:    "don't merge inline data",
			initial: &Content{Parts: []*Part{{Text: "hello"}}},
			newPart: &Part{InlineData: &Blob{MIMEType: "image/png"}},
			wantLen: 2,
		},
		{
			name:    "don't merge text to function call",
			initial: &Content{Parts: []*Part{{FunctionCall: &FunctionCall{Name: "test"}}}},
			newPart: &Part{Text: "hello"},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.initial.AddPart(tt.newPart)

			if len(tt.initial.Parts) != tt.wantLen {
				t.Fatalf("expected length %d, got %d", tt.wantLen, len(tt.initial.Parts))
			}

			if tt.wantText != "" {
				if tt.initial.Parts[0].Text != tt.wantText {
					t.Errorf("expected text %q, got %q", tt.wantText, tt.initial.Parts[0].Text)
				}
			}

			if tt.check != nil {
				tt.check(t, tt.initial)
			}
		})
	}
}

func TestPart_StructVerification(t *testing.T) {
	t.Parallel()
	// Just verify the fields exist as expected for the public API
	p := &Part{
		Text:             "text",
		Thought:          true,
		ThoughtSignature: []byte("sig"),
		AssetID:          "asset",
		InlineData:       &Blob{MIMEType: "mt", Data: []byte("d")},
		FunctionCall:     &FunctionCall{Name: "call", Args: map[string]interface{}{"a": 1}},
		FunctionResponse: &FunctionResponse{Name: "resp", Response: map[string]interface{}{"r": 2}},
	}

	if p.Text != "text" || !p.Thought || p.AssetID != "asset" {
		t.Error("field mismatch")
	}
	if !reflect.DeepEqual(p.ThoughtSignature, []byte("sig")) {
		t.Error("signature mismatch")
	}
}

func TestContent_Pinned(t *testing.T) {
	t.Parallel()
	c := &Content{
		Role:   "user",
		Pinned: true,
	}

	if !c.Pinned {
		t.Error("expected Pinned to be true")
	}

	c.Pinned = false
	if c.Pinned {
		t.Error("expected Pinned to be false")
	}
}
