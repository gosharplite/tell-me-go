// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"reflect"
	"testing"
)

func TestClone(t *testing.T) {
	tests := []struct {
		name string
		orig *Content
	}{
		{
			name: "full content with nested parts",
			orig: &Content{
				Role:       "user",
				TokenCount: 100,
				Pinned:     true,
				Parts: []*Part{
					{
						Text: "hello",
					},
					{
						InlineData: &Blob{
							MIMEType: "image/png",
							Data:     []byte{1, 2, 3},
						},
					},
					{
						FunctionCall: &FunctionCall{
							Name: "test_tool",
							Args: map[string]interface{}{
								"simple": "val",
								"nested": map[string]interface{}{
									"key": "val",
								},
								"list": []interface{}{1, 2, map[string]interface{}{"a": "b"}},
							},
						},
					},
					{
						FunctionResponse: &FunctionResponse{
							Name: "test_tool",
							Response: map[string]interface{}{
								"result": "ok",
							},
						},
					},
					{
						Text:             "thought",
						Thought:          "reasoning",
						ThoughtSignature: []byte("sig"),
						AssetID:          "asset-123",
					},
				},
				TransientParts: []*Part{
					{Text: "transient"},
				},
			},
		},
		{
			name: "nil slices and maps",
			orig: &Content{
				Role: "system",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clone := tt.orig.clone()

			// 1. Pointer inequality
			if tt.orig != nil && clone == tt.orig {
				t.Error("clone should not be the same pointer as original")
			}

			// 2. Deep equality
			if !reflect.DeepEqual(tt.orig, clone) {
				t.Error("clone should be deep equal to original")
			}

			// 3. Mutation independence
			if tt.orig != nil && clone != nil {
				verifyMutationIndependence(t, tt.orig, clone)
			}
		})
	}
}

func verifyMutationIndependence(t *testing.T, orig, clone *Content) {
	t.Helper()

	// Verify Parts slice independence
	if len(orig.Parts) > 0 {
		originalLen := len(orig.Parts)
		clone.Parts = append(clone.Parts, &Part{Text: "new"})
		if len(orig.Parts) != originalLen {
			t.Error("modifying clone.Parts affected original")
		}

		for i := range orig.Parts {
			verifyPartIndependence(t, orig.Parts[i], clone.Parts[i])
		}
	}

	// Verify TransientParts slice independence
	if len(orig.TransientParts) > 0 {
		originalLen := len(orig.TransientParts)
		clone.TransientParts = append(clone.TransientParts, &Part{Text: "new_transient"})
		if len(orig.TransientParts) != originalLen {
			t.Error("modifying clone.TransientParts affected original")
		}

		for i := range orig.TransientParts {
			verifyPartIndependence(t, orig.TransientParts[i], clone.TransientParts[i])
		}
	}
}

func verifyPartIndependence(t *testing.T, orig, clone *Part) {
	t.Helper()
	if orig == clone {
		t.Fatal("Part pointers are identical")
	}

	// InlineData byte slice mutation
	if orig.InlineData != nil && len(orig.InlineData.Data) > 0 {
		oldVal := orig.InlineData.Data[0]
		clone.InlineData.Data[0] = ^oldVal
		if orig.InlineData.Data[0] != oldVal {
			t.Error("modifying clone.InlineData.Data affected original")
		}
		clone.InlineData.Data[0] = oldVal // restore
	}

	// ThoughtSignature byte slice mutation
	if len(orig.ThoughtSignature) > 0 {
		oldVal := orig.ThoughtSignature[0]
		clone.ThoughtSignature[0] = ^oldVal
		if orig.ThoughtSignature[0] != oldVal {
			t.Error("modifying clone.ThoughtSignature affected original")
		}
		clone.ThoughtSignature[0] = oldVal // restore
	}

	// FunctionCall mutation
	if orig.FunctionCall != nil {
		verifyFunctionCallIndependence(t, orig.FunctionCall, clone.FunctionCall)
	}

	// FunctionResponse mutation
	if orig.FunctionResponse != nil {
		verifyFunctionResponseIndependence(t, orig.FunctionResponse, clone.FunctionResponse)
	}
}

func verifyFunctionCallIndependence(t *testing.T, orig, clone *FunctionCall) {
	t.Helper()
	if orig == clone {
		t.Fatal("FunctionCall pointers are identical")
	}

	if len(orig.Args) == 0 {
		return
	}

	// Verify map insertion independence
	clone.Args["__independence_test__"] = true
	if _, exists := orig.Args["__independence_test__"]; exists {
		t.Error("modifying clone.FunctionCall.Args map affected original (insertion)")
	}
	delete(clone.Args, "__independence_test__")

	// Verify deep map/slice independence
	for k, v := range orig.Args {
		verifyArgIndependence(t, k, v, clone.Args[k])
	}
}

func verifyArgIndependence(t *testing.T, key string, orig, clone interface{}) {
	t.Helper()
	switch val := orig.(type) {
	case map[string]interface{}:
		cMap := clone.(map[string]interface{})
		cMap["__nested_test__"] = true
		if _, exists := val["__nested_test__"]; exists {
			t.Errorf("modifying nested map in clone.FunctionCall.Args[%s] affected original", key)
		}
	case []interface{}:
		cSlice := clone.([]interface{})
		if len(val) > 0 {
			oldItem := val[0]
			cSlice[0] = "__mutated__"
			if val[0] != oldItem {
				t.Errorf("modifying nested slice in clone.FunctionCall.Args[%s] affected original", key)
			}
			cSlice[0] = oldItem // restore

			for i := range val {
				verifyArgIndependence(t, key, val[i], cSlice[i])
			}
		}
	}
}

func verifyFunctionResponseIndependence(t *testing.T, orig, clone *FunctionResponse) {
	t.Helper()
	if orig == clone {
		t.Fatal("FunctionResponse pointers are identical")
	}
	if len(orig.Response) > 0 {
		clone.Response["__independence_test__"] = true
		if _, exists := orig.Response["__independence_test__"]; exists {
			t.Error("modifying clone.FunctionResponse.Response map affected original")
		}
	}
}

func TestNilClones(t *testing.T) {
	var c *Content
	if c.clone() != nil {
		t.Error("cloning nil Content should return nil")
	}

	var p *Part
	if p.clone() != nil {
		t.Error("cloning nil Part should return nil")
	}

	var b *Blob
	if b.clone() != nil {
		t.Error("cloning nil Blob should return nil")
	}

	var fc *FunctionCall
	if fc.clone() != nil {
		t.Error("cloning nil FunctionCall should return nil")
	}

	var fr *FunctionResponse
	if fr.clone() != nil {
		t.Error("cloning nil FunctionResponse should return nil")
	}
}

func TestContentEqual(t *testing.T) {
	tests := []struct {
		name     string
		c1       *Content
		c2       *Content
		expected bool
	}{
		{
			name:     "Both nil",
			c1:       nil,
			c2:       nil,
			expected: true,
		},
		{
			name:     "One nil",
			c1:       &Content{Role: "user"},
			c2:       nil,
			expected: false,
		},
		{
			name:     "Different roles",
			c1:       &Content{Role: "user"},
			c2:       &Content{Role: "model"},
			expected: false,
		},
		{
			name:     "Different part counts",
			c1:       &Content{Role: "user", Parts: []*Part{{Text: "a"}}},
			c2:       &Content{Role: "user", Parts: []*Part{{Text: "a"}, {Text: "b"}}},
			expected: false,
		},
		{
			name:     "Same text content",
			c1:       &Content{Role: "user", Parts: []*Part{{Text: "hello"}}},
			c2:       &Content{Role: "user", Parts: []*Part{{Text: "hello"}}},
			expected: true,
		},
		{
			name:     "Different text content",
			c1:       &Content{Role: "user", Parts: []*Part{{Text: "hello"}}},
			c2:       &Content{Role: "user", Parts: []*Part{{Text: "world"}}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c1.equal(tt.c2); got != tt.expected {
				t.Errorf("Content.equal() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestPartEqual(t *testing.T) {
	tests := []struct {
		name     string
		p1       *Part
		p2       *Part
		expected bool
	}{
		{
			name:     "Both nil",
			p1:       nil,
			p2:       nil,
			expected: true,
		},
		{
			name:     "One nil",
			p1:       &Part{Text: "a"},
			p2:       nil,
			expected: false,
		},
		{
			name:     "Same text",
			p1:       &Part{Text: "a"},
			p2:       &Part{Text: "a"},
			expected: true,
		},
		{
			name:     "Different text",
			p1:       &Part{Text: "a"},
			p2:       &Part{Text: "b"},
			expected: false,
		},
		{
			name:     "Same thought",
			p1:       &Part{Text: "thinking", Thought: "yes"},
			p2:       &Part{Text: "thinking", Thought: "yes"},
			expected: true,
		},
		{
			name:     "Different thought",
			p1:       &Part{Text: "thinking", Thought: "yes"},
			p2:       &Part{Text: "thinking", Thought: "no"},
			expected: false,
		},
		{
			name: "Same inline data",
			p1: &Part{
				InlineData: &Blob{MIMEType: "image/png", Data: []byte("abc")},
			},
			p2: &Part{
				InlineData: &Blob{MIMEType: "image/png", Data: []byte("abc")},
			},
			expected: true,
		},
		{
			name: "Different inline data MIME",
			p1: &Part{
				InlineData: &Blob{MIMEType: "image/png", Data: []byte("abc")},
			},
			p2: &Part{
				InlineData: &Blob{MIMEType: "image/jpeg", Data: []byte("abc")},
			},
			expected: false,
		},
		{
			name: "Different inline data content",
			p1: &Part{
				InlineData: &Blob{MIMEType: "image/png", Data: []byte("abc")},
			},
			p2: &Part{
				InlineData: &Blob{MIMEType: "image/png", Data: []byte("def")},
			},
			expected: false,
		},
		{
			name: "Same function call",
			p1: &Part{
				FunctionCall: &FunctionCall{Name: "test", Args: map[string]interface{}{"a": 1}},
			},
			p2: &Part{
				FunctionCall: &FunctionCall{Name: "test", Args: map[string]interface{}{"a": 1}},
			},
			expected: true,
		},
		{
			name: "Different function call args",
			p1: &Part{
				FunctionCall: &FunctionCall{Name: "test", Args: map[string]interface{}{"a": 1}},
			},
			p2: &Part{
				FunctionCall: &FunctionCall{Name: "test", Args: map[string]interface{}{"a": 2}},
			},
			expected: false,
		},
		{
			name: "Same function response",
			p1: &Part{
				FunctionResponse: &FunctionResponse{Name: "test", Response: map[string]interface{}{"res": "ok"}},
			},
			p2: &Part{
				FunctionResponse: &FunctionResponse{Name: "test", Response: map[string]interface{}{"res": "ok"}},
			},
			expected: true,
		},
		{
			name:     "Same AssetID",
			p1:       &Part{AssetID: "asset-1"},
			p2:       &Part{AssetID: "asset-1"},
			expected: true,
		},
		{
			name:     "Different AssetID",
			p1:       &Part{AssetID: "asset-1"},
			p2:       &Part{AssetID: "asset-2"},
			expected: false,
		},
		{
			name:     "Same ThoughtSignature",
			p1:       &Part{ThoughtSignature: []byte("sig1")},
			p2:       &Part{ThoughtSignature: []byte("sig1")},
			expected: true,
		},
		{
			name:     "Different ThoughtSignature",
			p1:       &Part{ThoughtSignature: []byte("sig1")},
			p2:       &Part{ThoughtSignature: []byte("sig2")},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p1.equal(tt.p2); got != tt.expected {
				t.Errorf("Part.equal() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAddPart(t *testing.T) {
	tests := []struct {
		name     string
		initial  []*Part
		newPart  *Part
		expected []*Part
	}{
		{
			name:    "Append to empty",
			initial: nil,
			newPart: &Part{Text: "hello"},
			expected: []*Part{
				{Text: "hello"},
			},
		},
		{
			name: "Merge text",
			initial: []*Part{
				{Text: "hello "},
			},
			newPart: &Part{Text: "world"},
			expected: []*Part{
				{Text: "hello world"},
			},
		},
		{
			name: "Merge thoughts",
			initial: []*Part{
				{Text: "thinking...", Thought: "step 1"},
			},
			newPart: &Part{Text: " done", Thought: " step 2"},
			expected: []*Part{
				{Text: "thinking... done", Thought: "step 1 step 2"},
			},
		},
		{
			name: "Do not merge different types (text then thought)",
			initial: []*Part{
				{Text: "hello"},
			},
			newPart: &Part{Text: "thinking", Thought: "step 1"},
			expected: []*Part{
				{Text: "hello"},
				{Text: "thinking", Thought: "step 1"},
			},
		},
		{
			name: "Do not merge different types (thought then text)",
			initial: []*Part{
				{Text: "thinking", Thought: "step 1"},
			},
			newPart: &Part{Text: "hello"},
			expected: []*Part{
				{Text: "thinking", Thought: "step 1"},
				{Text: "hello"},
			},
		},
		{
			name: "Do not merge if blobs present",
			initial: []*Part{
				{InlineData: &Blob{MIMEType: "text/plain", Data: []byte("foo")}},
			},
			newPart: &Part{Text: "bar"},
			expected: []*Part{
				{InlineData: &Blob{MIMEType: "text/plain", Data: []byte("foo")}},
				{Text: "bar"},
			},
		},
		{
			name:    "Preserve metadata when appending",
			initial: nil,
			newPart: &Part{
				Text:             "some text",
				AssetID:          "asset-123",
				ThoughtSignature: []byte("sig-456"),
			},
			expected: []*Part{
				{
					Text:             "some text",
					AssetID:          "asset-123",
					ThoughtSignature: []byte("sig-456"),
				},
			},
		},
		{
			name: "Idempotent thought merging",
			initial: []*Part{
				{Thought: "true"},
			},
			newPart: &Part{Thought: "true"},
			expected: []*Part{
				{Thought: "true"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Content{Parts: tt.initial}
			c.AddPart(tt.newPart)

			if len(c.Parts) != len(tt.expected) {
				t.Fatalf("expected %d parts, got %d", len(tt.expected), len(c.Parts))
			}

			for i := range c.Parts {
				if !c.Parts[i].equal(tt.expected[i]) {
					t.Errorf("part %d mismatch: got %+v, want %+v", i, c.Parts[i], tt.expected[i])
				}
			}
		})
	}
}
