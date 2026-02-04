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
						Thought:          true,
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
			clone := tt.orig.Clone()

			// 1. Pointer inequality
			if tt.orig != nil && clone == tt.orig {
				t.Error("clone should not be the same pointer as original")
			}

			// 2. Deep equality
			if !reflect.DeepEqual(tt.orig, clone) {
				t.Error("clone should be deep equal to original")
			}

			// 3. Mutation independence
			if len(tt.orig.Parts) > 0 {
				// Modify slice
				originalLen := len(tt.orig.Parts)
				clone.Parts = append(clone.Parts, &Part{Text: "new"})
				if len(tt.orig.Parts) != originalLen {
					t.Error("modifying clone.Parts should not affect tt.orig.Parts")
				}

				// Modify nested map in FunctionCall
				for _, p := range clone.Parts {
					if p.FunctionCall != nil {
						fc := p.FunctionCall
						if nested, ok := fc.Args["nested"].(map[string]interface{}); ok {
							nested["key"] = "mutated"
						}
						if list, ok := fc.Args["list"].([]interface{}); ok {
							list[0] = 999
							if nestedInList, ok := list[2].(map[string]interface{}); ok {
								nestedInList["a"] = "mutated"
							}
						}
					}
				}

				// Check original is unchanged
				for _, p := range tt.orig.Parts {
					if p.FunctionCall != nil {
						fc := p.FunctionCall
						if nested, ok := fc.Args["nested"].(map[string]interface{}); ok {
							if nested["key"] != "val" {
								t.Error("modifying nested map in clone affected original")
							}
						}
						if list, ok := fc.Args["list"].([]interface{}); ok {
							if list[0] != 1 {
								t.Error("modifying nested slice in clone affected original")
							}
							if nestedInList, ok := list[2].(map[string]interface{}); ok {
								if nestedInList["a"] != "b" {
									t.Error("modifying nested map in nested slice in clone affected original")
								}
							}
						}
					}
				}

				// Modify byte slice
				for _, p := range clone.Parts {
					if p.InlineData != nil {
						p.InlineData.Data[0] = 255
					}
					if len(p.ThoughtSignature) > 0 {
						p.ThoughtSignature[0] = 0
					}
				}

				for _, p := range tt.orig.Parts {
					if p.InlineData != nil {
						if p.InlineData.Data[0] != 1 {
							t.Error("modifying byte slice in clone affected original")
						}
					}
					if len(p.ThoughtSignature) > 0 {
						if p.ThoughtSignature[0] != 's' { // "sig"[0] is 's'
							t.Error("modifying thought signature in clone affected original")
						}
					}
				}
			}
		})
	}
}

func TestNilClones(t *testing.T) {
	var c *Content
	if c.Clone() != nil {
		t.Error("cloning nil Content should return nil")
	}

	var p *Part
	if p.Clone() != nil {
		t.Error("cloning nil Part should return nil")
	}

	var b *Blob
	if b.Clone() != nil {
		t.Error("cloning nil Blob should return nil")
	}

	var fc *FunctionCall
	if fc.Clone() != nil {
		t.Error("cloning nil FunctionCall should return nil")
	}

	var fr *FunctionResponse
	if fr.Clone() != nil {
		t.Error("cloning nil FunctionResponse should return nil")
	}
}
