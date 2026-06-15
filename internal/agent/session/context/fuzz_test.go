// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// seedConstructors maps fuzz seed names to their concrete []*llm.Content fixtures.
// Each function is a closure that returns the identical data previously produced
// by the buildSeedContents switch statement.
var seedConstructors = map[string]func() []*llm.Content{
	"empty":     func() []*llm.Content { return []*llm.Content{} },
	"nil_slice": func() []*llm.Content { return nil },
	"one_nil_content": func() []*llm.Content {
		return []*llm.Content{nil}
	},
	"nil_parts_slices": func() []*llm.Content {
		return []*llm.Content{{Role: "user"}}
	},
	"nil_part_in_parts": func() []*llm.Content {
		return []*llm.Content{{
			Role:  "user",
			Parts: []*llm.Part{nil, {Text: "hello"}},
		}}
	},
	"nil_part_in_transient": func() []*llm.Content {
		return []*llm.Content{{
			Role:           "user",
			TransientParts: []*llm.Part{nil, {Text: "transient"}},
		}}
	},
	"text_only": func() []*llm.Content {
		return []*llm.Content{{
			Role:  "user",
			Parts: []*llm.Part{{Text: "hello world"}},
		}}
	},
	"function_call": func() []*llm.Content {
		return []*llm.Content{{
			Role: "model",
			Parts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{
					Name: "search",
					Args: map[string]interface{}{"q": "test", "n": float64(5)},
				},
			}},
		}}
	},
	"function_response": func() []*llm.Content {
		return []*llm.Content{{
			Role: "tool",
			Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{
					Name:     "search",
					Response: map[string]interface{}{"ok": true, "count": float64(3)},
				},
			}},
		}}
	},
	"inline_data": func() []*llm.Content {
		return []*llm.Content{{
			Role: "user",
			Parts: []*llm.Part{{
				InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("fake")},
			}},
		}}
	},
	"transient_only": func() []*llm.Content {
		return []*llm.Content{{
			Role:           "user",
			Parts:          []*llm.Part{},
			TransientParts: []*llm.Part{{Text: "transient instruction"}},
		}}
	},
	"mixed_all_types": func() []*llm.Content {
		return []*llm.Content{{
			Role: "model",
			Parts: []*llm.Part{{
				Text: "mixed",
				FunctionCall: &llm.FunctionCall{
					Name: "multi",
					Args: map[string]interface{}{"x": float64(1)},
				},
				FunctionResponse: &llm.FunctionResponse{
					Name:     "multi",
					Response: map[string]interface{}{"y": "z"},
				},
				InlineData: &llm.Blob{MIMEType: "text/plain", Data: []byte("data")},
			}},
			TransientParts: []*llm.Part{{Text: "transient"}},
		}}
	},
	"nil_functioncall_args": func() []*llm.Content {
		return []*llm.Content{{
			Role: "model",
			Parts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{Name: "f", Args: nil},
			}},
		}}
	},
	"nil_functionresponse_response": func() []*llm.Content {
		return []*llm.Content{{
			Role: "tool",
			Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{Name: "g", Response: nil},
			}},
		}}
	},
	"nested_maps": func() []*llm.Content {
		return []*llm.Content{{
			Role: "tool",
			Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{
					Name: "nest",
					Response: map[string]interface{}{
						"a": map[string]interface{}{
							"b": map[string]interface{}{
								"c": "d",
							},
						},
					},
				},
			}},
		}}
	},
	"mixed_slice_types": func() []*llm.Content {
		return []*llm.Content{{
			Role: "tool",
			Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{
					Name: "mix",
					Response: map[string]interface{}{
						"items": []interface{}{"str", float64(1), true, nil},
					},
				},
			}},
		}}
	},
	"multiple_contents": func() []*llm.Content {
		return []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
			{Role: "model", Parts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{
					Name: "f",
					Args: map[string]interface{}{"x": "y"},
				},
			}}},
			{Role: "user", Parts: []*llm.Part{{Text: "thanks"}}},
		}
	},
	"large_text": func() []*llm.Content {
		large := make([]byte, 10000)
		for i := range large {
			large[i] = 'a'
		}
		return []*llm.Content{{
			Role:  "user",
			Parts: []*llm.Part{{Text: string(large)}},
		}}
	},
	"circular_map": func() []*llm.Content {
		selfRef := map[string]interface{}{}
		selfRef["self"] = selfRef
		return []*llm.Content{{
			Role: "tool",
			Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{
					Name:     "cycle",
					Response: selfRef,
				},
			}},
		}}
	},
	"overflow_many_contents": func() []*llm.Content {
		n := 1000
		contents := make([]*llm.Content, n)
		for i := 0; i < n; i++ {
			role := "user"
			if i%2 == 1 {
				role = "model"
			}
			text := make([]byte, 1024)
			for j := range text {
				text[j] = byte('a' + (i+j)%26)
			}
			contents[i] = &llm.Content{
				Role: role,
				Parts: []*llm.Part{
					{Text: string(text)},
					{
						FunctionCall: &llm.FunctionCall{
							Name: "f",
							Args: map[string]interface{}{
								"input":   string(text),
								"count":   float64(i),
								"enabled": i%2 == 0,
								"nested": map[string]interface{}{
									"key": "value",
								},
							},
						},
					},
				},
			}
		}
		return contents
	},
	"exotic_map_types": func() []*llm.Content {
		return []*llm.Content{{
			Role: "tool",
			Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{
					Name: "exotic",
					Response: map[string]interface{}{
						"int32_val":    int32(42),
						"float32_val":  float32(3.14),
						"uint_val":     uint(100),
						"byte_slice":   []byte("raw bytes"),
						"string_slice": []string{"a", "b", "c"},
						"empty_slice":  []interface{}{},
						"deep_nest": map[string]interface{}{
							"l2": map[string]interface{}{
								"l3": map[string]interface{}{
									"l4": map[string]interface{}{
										"l5": "deep",
									},
								},
							},
						},
					},
				},
			}},
		}}
	},
}

// FuzzTokenCounter verifies that HeuristicTokenCounter.Count never panics
// and always produces a non-negative result across all nil-variant scenarios,
// randomized Part types, and nil Content pointers.
func FuzzTokenCounter(f *testing.F) {
	// Seed corpus: descriptive labels mapped to concrete structures in buildSeedContents.
	f.Add("empty")
	f.Add("nil_slice")
	f.Add("one_nil_content")
	f.Add("nil_parts_slices")
	f.Add("nil_part_in_parts")
	f.Add("nil_part_in_transient")
	f.Add("text_only")
	f.Add("function_call")
	f.Add("function_response")
	f.Add("inline_data")
	f.Add("transient_only")
	f.Add("mixed_all_types")
	f.Add("nil_functioncall_args")
	f.Add("nil_functionresponse_response")
	f.Add("nested_maps")
	f.Add("mixed_slice_types")
	f.Add("multiple_contents")
	f.Add("large_text")
	f.Add("circular_map")
	f.Add("overflow_many_contents")
	f.Add("exotic_map_types")

	f.Fuzz(func(t *testing.T, seedName string) {
		contents := buildSeedContents(seedName)

		// Test with nil registry
		counterNoReg := NewHeuristicTokenCounter(nil)
		resultNoReg := counterNoReg.Count(contents)
		verifyInvariants(t, counterNoReg, contents, resultNoReg)

		// Test with stub registry
		reg := &stubRegistry{decls: makeToolDecls(5)}
		counterWithReg := NewHeuristicTokenCounter(reg)
		resultWithReg := counterWithReg.Count(contents)
		verifyInvariants(t, counterWithReg, contents, resultWithReg)

		// Registry must not decrease count
		if resultWithReg < resultNoReg {
			t.Errorf("registry decreased count: with=%d, without=%d", resultWithReg, resultNoReg)
		}
	})
}

// buildSeedContents maps a seed label to a concrete []*llm.Content structure.
// The default branch handles fuzzer-generated mutations by using the seed
// string directly as text content.
func buildSeedContents(seed string) []*llm.Content {
	if fn, ok := seedConstructors[seed]; ok {
		return fn()
	}
	return []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: seed}}}}
}

// verifyInvariants checks that Count results satisfy basic correctness properties.
func verifyInvariants(t *testing.T, counter *HeuristicTokenCounter, contents []*llm.Content, result int) {
	t.Helper()

	// Invariant 1: non-negative
	if result < 0 {
		t.Errorf("Count returned negative: %d", result)
	}

	// Invariant 2: TokenCount cache side-effect
	for i, c := range contents {
		if c == nil {
			continue // nil Content is skipped
		}
		if len(c.TransientParts) == 0 && len(c.Parts) > 0 {
			if c.TokenCount < 0 {
				t.Errorf("content[%d]: TokenCount not cached (got %d) when TransientParts empty and Parts non-empty", i, c.TokenCount)
			}
		}
	}
}
