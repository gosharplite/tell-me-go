// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

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
	switch seed {
	case "empty":
		return []*llm.Content{}
	case "nil_slice":
		return nil
	case "one_nil_content":
		return []*llm.Content{nil}
	case "nil_parts_slices":
		return []*llm.Content{{Role: "user"}}
	case "nil_part_in_parts":
		return []*llm.Content{{
			Role:  "user",
			Parts: []*llm.Part{nil, {Text: "hello"}},
		}}
	case "nil_part_in_transient":
		return []*llm.Content{{
			Role:           "user",
			TransientParts: []*llm.Part{nil, {Text: "transient"}},
		}}
	case "text_only":
		return []*llm.Content{{
			Role:  "user",
			Parts: []*llm.Part{{Text: "hello world"}},
		}}
	case "function_call":
		return []*llm.Content{{
			Role: "model",
			Parts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{
					Name: "search",
					Args: map[string]interface{}{"q": "test", "n": float64(5)},
				},
			}},
		}}
	case "function_response":
		return []*llm.Content{{
			Role: "tool",
			Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{
					Name:     "search",
					Response: map[string]interface{}{"ok": true, "count": float64(3)},
				},
			}},
		}}
	case "inline_data":
		return []*llm.Content{{
			Role: "user",
			Parts: []*llm.Part{{
				InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("fake")},
			}},
		}}
	case "transient_only":
		return []*llm.Content{{
			Role:           "user",
			Parts:          []*llm.Part{},
			TransientParts: []*llm.Part{{Text: "transient instruction"}},
		}}
	case "mixed_all_types":
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
	case "nil_functioncall_args":
		return []*llm.Content{{
			Role: "model",
			Parts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{Name: "f", Args: nil},
			}},
		}}
	case "nil_functionresponse_response":
		return []*llm.Content{{
			Role: "tool",
			Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{Name: "g", Response: nil},
			}},
		}}
	case "nested_maps":
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
	case "mixed_slice_types":
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
	case "multiple_contents":
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
	case "large_text":
		large := make([]byte, 10000)
		for i := range large {
			large[i] = 'a'
		}
		return []*llm.Content{{
			Role:  "user",
			Parts: []*llm.Part{{Text: string(large)}},
		}}
	case "circular_map":
		// Self-referencing map: exercises unbounded recursion path (R2).
		// This should NOT panic or stack-overflow; if it does, the fuzz
		// framework will report the crash.
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
	case "overflow_many_contents":
		// 1000 contents each with a 1KB text + function call with args.
		// Stresses totalTokens accumulation. On 64-bit systems this is
		// harmless (~1000 * 350 ≈ 350K tokens); the seed validates no
		// panic and non-negative result under bulk input.
		n := 1000
		contents := make([]*llm.Content, n)
		for i := 0; i < n; i++ {
			role := "user"
			if i%2 == 1 {
				role = "model"
			}
			// ~1KB text
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

	case "exotic_map_types":
		// Maps with types that fall through to the default branch (20 chars)
		// of estimateValueSizeInternal. Confirms no type-assertion panic.
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
						"string_slice": []string{"a", "b", "c"}, // NOT []interface{}
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
	default:
		// Fuzzer-generated mutations: use seed as text to explore the string space.
		return []*llm.Content{{
			Role:  "user",
			Parts: []*llm.Part{{Text: seed}},
		}}
	}
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
