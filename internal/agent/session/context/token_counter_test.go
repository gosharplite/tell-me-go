// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Count tests
// ---------------------------------------------------------------------------

func TestCount_TransientParts(t *testing.T) {
	counter := NewHeuristicTokenCounter(nil)

	// Content with one text Part and one TransientPart
	contentWithTransients := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{Text: "hello"},
		},
		TransientParts: []*llm.Part{
			{Text: "world"},
		},
	}

	// Same Parts but no TransientParts
	contentNoTransients := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{Text: "hello"},
		},
	}

	resultWithTransients := counter.Count([]*llm.Content{contentWithTransients})
	resultNoTransients := counter.Count([]*llm.Content{contentNoTransients})

	// TransientParts characters were counted: result should be higher
	require.Greater(t, resultWithTransients, resultNoTransients,
		"TransientParts should contribute to token count")

	// TokenCount is NOT cached when TransientParts are present
	require.Equal(t, 0, contentWithTransients.TokenCount,
		"TokenCount must not be cached when TransientParts are present")
}

func TestCount_NoTransientParts_CachesTokenCount(t *testing.T) {
	counter := NewHeuristicTokenCounter(nil)

	content := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{Text: "hello world this is a test"},
		},
		// TransientParts is nil / zero-length
	}

	_ = counter.Count([]*llm.Content{content})

	// TokenCount must be cached (positive) when TransientParts are absent
	require.Greater(t, content.TokenCount, 0,
		"TokenCount must be cached when TransientParts are empty")
}

func TestCount_RegistryTools(t *testing.T) {
	counterNoReg := NewHeuristicTokenCounter(nil)

	// Create a stub registry with tool declarations that contribute tokens
	reg := &stubRegistry{
		decls: []*tools.ToolDeclaration{
			{
				Name:        "read_file",
				Description: "Reads the content of a file at the given path.",
				Parameters: &tools.Schema{
					Type: "object",
					Properties: map[string]*tools.Schema{
						"path": {Type: "string", Description: "The file path to read."},
					},
				},
			},
		},
	}
	counterWithReg := NewHeuristicTokenCounter(reg)

	contents := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
	}

	resultNoReg := counterNoReg.Count(contents)
	resultWithReg := counterWithReg.Count(contents)

	// Tool declarations must contribute to the total
	require.Greater(t, resultWithReg, resultNoReg,
		"Registry with tool declarations must increase token count")
}

// ---------------------------------------------------------------------------
// estimatePartChars tests
// ---------------------------------------------------------------------------

func TestEstimatePartChars_NilPart(t *testing.T) {
	counter := NewHeuristicTokenCounter(nil)

	chars := counter.estimatePartChars(nil)

	require.Equal(t, 0, chars, "nil Part must return 0 chars")
}

func TestEstimatePartChars_FunctionResponse(t *testing.T) {
	counter := NewHeuristicTokenCounter(nil)

	resp := map[string]interface{}{
		"status":  "ok",
		"count":   float64(42),
		"enabled": true,
		"tags":    []interface{}{"a", "bb"},
	}

	part := &llm.Part{
		FunctionResponse: &llm.FunctionResponse{
			Name:     "search_files",
			Response: resp,
		},
	}

	chars := counter.estimatePartChars(part)

	// Expected: len("search_files") = 12
	// + estimateMapSizeInternal(resp)
	//   "status"(6) + "ok"(2) = 8
	//   "count"(5) + float64→10 = 15
	//   "enabled"(7) + bool→5 = 12
	//   "tags"(4) + []interface{}(1 + "a"(1) + "bb"(2)) = 8
	//   total map = 43
	// = 12 + 43 = 55
	expected := 55

	require.Equal(t, expected, chars,
		"FunctionResponse chars must include Name length + response map size")
}

func TestEstimatePartChars_InlineData(t *testing.T) {
	counter := NewHeuristicTokenCounter(nil)

	part := &llm.Part{
		InlineData: &llm.Blob{
			MIMEType: "image/png",
			Data:     []byte("mock-data"),
		},
	}

	chars := counter.estimatePartChars(part)

	require.Equal(t, 160, chars,
		"InlineData must contribute exactly 160 character-equivalents")
}

// ---------------------------------------------------------------------------
// estimateMapSizeInternal tests
// ---------------------------------------------------------------------------

func TestEstimateMapSizeInternal_NilMap(t *testing.T) {
	size := estimateMapSizeInternal(nil, 0)

	require.Equal(t, 0, size, "nil map must return 0")
}

// ---------------------------------------------------------------------------
// estimateValueSizeInternal tests (table-driven)
// ---------------------------------------------------------------------------

func TestEstimateValueSizeInternal_AllTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected int
	}{
		{"nil", nil, 4},
		{"string", "hello", 5},
		{"float64", float64(3.14), 10},
		{"int", int(42), 10},
		{"int64", int64(99), 10},
		{"bool true", true, 5},
		{"bool false", false, 5},
		{
			name:     "map string to string",
			input:    map[string]interface{}{"k": "v"},
			expected: len("k") + len("v"), // 1 + 1 = 2
		},
		{
			name:     "slice of strings",
			input:    []interface{}{"ab", "cd"},
			expected: 1 + len("ab") + len("cd"), // 1 + 2 + 2 = 5
		},
		{
			name:     "unknown type (struct)",
			input:    struct{}{},
			expected: 20,
		},
		{
			name: "nested map",
			input: map[string]interface{}{
				"outer": map[string]interface{}{
					"inner": "val",
				},
			},
			// "outer"(5) + ("inner"(5) + "val"(3)) = 13
			expected: len("outer") + len("inner") + len("val"),
		},
		{
			name:     "slice with mixed types",
			input:    []interface{}{"x", float64(0), true},
			expected: 1 + len("x") + 10 + 5, // 1 + 1 + 10 + 5 = 17
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateValueSizeInternal(tt.input, 0)
			require.Equal(t, tt.expected, got,
				"estimateValueSizeInternal(%v)", tt.input)
		})
	}
}

// ---------------------------------------------------------------------------
// estimatePartChars integration tests (table-driven)
// ---------------------------------------------------------------------------

func TestEstimatePartChars_AllPartTypes(t *testing.T) {
	counter := NewHeuristicTokenCounter(nil)

	tests := []struct {
		name     string
		part     *llm.Part
		expected int
	}{
		{
			name:     "empty part",
			part:     &llm.Part{},
			expected: 0,
		},
		{
			name:     "text only",
			part:     &llm.Part{Text: "hello world"},
			expected: len("hello world"), // 11
		},
		{
			name: "function call with args",
			part: &llm.Part{
				FunctionCall: &llm.FunctionCall{
					Name: "do_stuff",
					Args: map[string]interface{}{
						"param": "value",
						"num":   float64(1),
					},
				},
			},
			// len("do_stuff")=8 + ("param"(5)+"value"(5)) + ("num"(3)+float64→10) = 31
			expected: len("do_stuff") + len("param") + len("value") + len("num") + 10,
		},
		{
			name: "function response",
			part: &llm.Part{
				FunctionResponse: &llm.FunctionResponse{
					Name: "fn",
					Response: map[string]interface{}{
						"ok": true,
					},
				},
			},
			// len("fn")=2 + ("ok"(2)+bool→5) = 9
			expected: len("fn") + len("ok") + 5,
		},
		{
			name: "inline data only",
			part: &llm.Part{
				InlineData: &llm.Blob{MIMEType: "image/png"},
			},
			expected: 160,
		},
		{
			name: "mix of all types",
			part: &llm.Part{
				Text: "abc",
				FunctionCall: &llm.FunctionCall{
					Name: "f",
					Args: map[string]interface{}{
						"x": "y",
					},
				},
				FunctionResponse: &llm.FunctionResponse{
					Name: "g",
					Response: map[string]interface{}{
						"z": float64(0),
					},
				},
				InlineData: &llm.Blob{MIMEType: "image/png"},
			},
			// Text(3) + fc(len("f")=1 + "x"(1)+"y"(1)) + fr(len("g")=1 + "z"(1)+10) + inline(160)
			// = 3 + 3 + 12 + 160 = 178
			expected: len("abc") + len("f") + len("x") + len("y") +
				len("g") + len("z") + 10 + 160,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := counter.estimatePartChars(tt.part)
			require.Equal(t, tt.expected, got,
				"estimatePartChars returned unexpected value for %s", tt.name)
		})
	}
}

// ---------------------------------------------------------------------------
// Deep recursion guard tests
// ---------------------------------------------------------------------------

// buildDeepMap constructs a deeply nested map[string]interface{}.
// buildDeepMap(0) returns {"k": "v"} (1 level).
// buildDeepMap(n) returns a map with n+1 levels of nesting.
func buildDeepMap(depth int) map[string]interface{} {
	if depth <= 0 {
		return map[string]interface{}{"k": "v"}
	}
	inner := buildDeepMap(depth - 1)
	return map[string]interface{}{"k": inner}
}

// TestEstimateMapSizeInternal_DeepRecursionGuard verifies the recursion
// breaker at token_counter.go:111-113. When a map exceeds maxEstimateDepth
// (100), estimateMapSizeInternal must return 0 instead of continuing to
// recurse. The "at boundary" subtest proves normal estimation works at
// exactly 100 levels; the "exceeds boundary" subtest proves the guard
// triggers at 101 levels, preventing a stack overflow panic.
func TestEstimateMapSizeInternal_DeepRecursionGuard(t *testing.T) {
	t.Parallel()

	t.Run("at_max_depth_returns_normal_estimate", func(t *testing.T) {
		// 100 levels deep = exactly at maxEstimateDepth
		m := buildDeepMap(maxEstimateDepth - 1)
		result := estimateMapSizeInternal(m, 0)

		require.Greater(t, result, 0,
			"at exactly maxEstimateDepth, estimate must be > 0")
	})

	t.Run("exceeds_max_depth_returns_zero", func(t *testing.T) {
		// Call estimateMapSizeInternal directly with depth > maxEstimateDepth.
		// Through normal recursion the estimateValueSizeInternal guard (at
		// odd depths) fires before estimateMapSizeInternal's guard (at even
		// depths), so a direct call with excessive depth is needed to cover
		// token_counter.go:111-113.
		m := map[string]interface{}{"key": "value"}
		result := estimateMapSizeInternal(m, maxEstimateDepth+1)

		require.Equal(t, 0, result,
			"when depth exceeds maxEstimateDepth, must return 0")
	})
}
