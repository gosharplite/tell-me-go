// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"testing"
)

// TestResolveCapabilities_MaxTokensField pins the enum value returned
// by ResolveCapabilities for representative models from each tier.
//
// The three wire-format choices for the per-request output-token
// budget are mutually exclusive at the API layer:
//
//   - max_tokens               → DeepSeek (all variants), legacy
//     OpenAI Chat Completions models.
//   - max_completion_tokens    → OpenAI o-series and gpt-5.0..gpt-5.3
//     on /chat/completions.
//   - max_output_tokens        → OpenAI gpt-5.4+ on /responses.
//
// Encoding these as an enum (rather than two coupled booleans, the
// pre-Task-2 design) makes invalid combinations unrepresentable and
// is the structural fix for the latent defect repaired in 8753a662.
//
// FAILURE MEANING: If any row asserts the wrong enum value, the
// OpenAI client will silently send the wrong field name to the
// upstream API and every request from that model will be rejected
// with HTTP 400 "unsupported_parameter" (or, worse, silently
// truncated when the field is omitted entirely).
func TestResolveCapabilities_MaxTokensField(t *testing.T) {
	tests := []struct {
		model    string
		expected MaxTokensField
	}{
		{model: "gpt-4", expected: MaxTokensFieldLegacy},
		{model: "gpt-5", expected: MaxTokensFieldCompletion},
		{model: "gpt-5.4", expected: MaxTokensFieldOutput},
		{model: "gpt-6", expected: MaxTokensFieldOutput},
		{model: "o1-mini", expected: MaxTokensFieldCompletion},
		{model: "o3", expected: MaxTokensFieldCompletion},
		{model: "deepseek-reasoner", expected: MaxTokensFieldLegacy},
		{model: "deepseek-ai/deepseek-v3.2-maas", expected: MaxTokensFieldLegacy},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			caps := ResolveCapabilities(tt.model, "")
			if caps.MaxTokensField != tt.expected {
				t.Errorf("ResolveCapabilities(%q).MaxTokensField = %d; want %d",
					tt.model, caps.MaxTokensField, tt.expected)
			}
		})
	}
}

// TestCapabilities_NoBothBudgetFieldsAtOnce documents the structural
// invariant that the legacy `UseMaxCompletionTokens` boolean is gone.
//
// The assertion is COMPILE-TIME enforced: if a future commit
// re-introduces a `UseMaxCompletionTokens bool` field on the
// Capabilities struct, this test (which deliberately does NOT
// reference it) will continue to compile but the invariant comment
// below will be stale. Conversely, any attempt to *use*
// caps.UseMaxCompletionTokens elsewhere in the codebase WILL fail
// to compile, which is the real enforcement mechanism.
//
// We exercise ResolveCapabilities for every model in the table to
// ensure no panic from a partially-initialized enum value, and to
// make this test's intent unambiguous when grep'ing for
// "UseMaxCompletionTokens" hits this comment block.
func TestCapabilities_NoBothBudgetFieldsAtOnce(t *testing.T) {
	models := []string{
		"gpt-4", "gpt-5", "gpt-5.4", "gpt-6",
		"o1-mini", "o3",
		"deepseek-reasoner", "deepseek-ai/deepseek-v3.2-maas",
	}
	for _, m := range models {
		caps := ResolveCapabilities(m, "")
		// Sanity: the enum must land in one of the three documented values.
		switch caps.MaxTokensField {
		case MaxTokensFieldLegacy, MaxTokensFieldCompletion, MaxTokensFieldOutput:
			// ok
		default:
			t.Errorf("model %q produced out-of-range MaxTokensField=%d",
				m, caps.MaxTokensField)
		}
	}
}
