// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// HeuristicTokenCounter provides a rule-of-thumb token estimation.
type HeuristicTokenCounter struct {
	registry tools.Registry
}

// NewHeuristicTokenCounter creates a new heuristic-based counter.
func NewHeuristicTokenCounter(registry tools.Registry) *HeuristicTokenCounter {
	return &HeuristicTokenCounter{registry: registry}
}

// Count estimates tokens based on character counts and tool declarations.
func (c *HeuristicTokenCounter) Count(contents []*llm.Content) int {
	totalTokens := 0

	// Overhead for tools if registry is provided
	if c.registry != nil {
		for _, decl := range c.registry.GetDeclarations() {
			totalTokens += (len(decl.Name) + len(decl.Description)) / 4
			if decl.Parameters != nil {
				totalTokens += 50 // Heuristic for parameter definitions
			}
		}
	}

	for _, content := range contents {
		charCount := 0
		for _, p := range content.Parts {
			charCount += c.estimatePartChars(p)
		}
		for _, p := range content.TransientParts {
			charCount += c.estimatePartChars(p)
		}

		// Heuristic: ~3.2 chars per token
		tokenCount := int(float64(charCount) / 3.2)

		// Cache the token count only if there are no transient parts,
		// otherwise the cache would be incorrect for subsequent calls without transients.
		// Actually, since transients are used in the Prepare phase, they might vary.
		if len(content.TransientParts) == 0 {
			content.TokenCount = tokenCount
		}

		totalTokens += tokenCount
	}

	totalTokens += 300 // Base overhead
	return totalTokens
}

func (c *HeuristicTokenCounter) estimatePartChars(p *llm.Part) int {
	if p == nil {
		return 0
	}
	charCount := 0
	if p.Text != "" {
		charCount += len(p.Text)
	}
	if p.FunctionCall != nil {
		charCount += len(p.FunctionCall.Name)
		charCount += estimateMapSizeInternal(p.FunctionCall.Args)
	}
	if p.FunctionResponse != nil {
		charCount += len(p.FunctionResponse.Name)
		charCount += estimateMapSizeInternal(p.FunctionResponse.Response)
	}
	if p.InlineData != nil {
		charCount += 160 // Heuristic for blob (roughly 50 tokens)
	}
	return charCount
}

func estimateMapSizeInternal(m map[string]interface{}) int {
	if m == nil {
		return 0
	}
	size := 0
	for k, v := range m {
		size += len(k)
		size += estimateValueSizeInternal(v)
	}
	return size
}

func estimateValueSizeInternal(v interface{}) int {
	if v == nil {
		return 4
	}
	switch val := v.(type) {
	case string:
		return len(val)
	case float64, int, int64:
		return 10
	case bool:
		return 5
	case map[string]interface{}:
		return estimateMapSizeInternal(val)
	case []interface{}:
		size := 1
		for _, item := range val {
			size += estimateValueSizeInternal(item)
		}
		return size
	default:
		return 20
	}
}
