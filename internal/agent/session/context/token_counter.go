// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// maxEstimateDepth limits recursion in estimateMapSizeInternal and
// estimateValueSizeInternal to prevent stack overflow from circular
// map references (e.g., m["self"] = m).
const maxEstimateDepth = 100

// nilValueSize is the estimated character-equivalent for a nil value.
// Nil represents the absence of data; 4 matches a short literal like "null".
const nilValueSize = 4

// boolValueSize is the estimated character-equivalent for a boolean.
// Booleans serialize to either "true" (4 chars) or "false" (5 chars);
// 5 covers the larger representation.
const boolValueSize = 5

// numericValueSize is the estimated character-equivalent for a numeric
// value (float64, int, int64). 10 characters accommodates typical decimal
// representations including optional sign, integer digits, and decimal point.
const numericValueSize = 10

// unknownValueSize is the estimated character-equivalent for any value
// whose type is not explicitly handled. 20 characters is a conservative
// over-estimate to avoid under-counting tokens for unexpected shapes.
const unknownValueSize = 20

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
	totalTokens := c.countToolDeclarationOverhead()

	for _, content := range contents {
		totalTokens += c.countContentTokens(content)
	}

	totalTokens += 300 // Base overhead
	return totalTokens
}

// countContentTokens estimates tokens for a single Content entry.
// It accumulates character counts from both Parts and TransientParts,
// converts to tokens using the ~3.2 chars/token heuristic, and —
// as a side effect — caches the result in content.TokenCount when
// TransientParts is empty. Nil content returns 0.
func (c *HeuristicTokenCounter) countContentTokens(content *llm.Content) int {
	if content == nil {
		return 0
	}
	charCount := 0
	charCount += c.accumulatePartsChars(content.Parts)
	charCount += c.accumulatePartsChars(content.TransientParts)

	// Heuristic: ~3.2 chars per token
	tokenCount := int(float64(charCount) / 3.2)

	// Cache the token count only if there are no transient parts,
	// otherwise the cache would be incorrect for subsequent calls without transients.
	if len(content.TransientParts) == 0 {
		content.TokenCount = tokenCount
	}

	return tokenCount
}

// accumulatePartsChars sums estimatePartChars across a slice of Parts.
func (c *HeuristicTokenCounter) accumulatePartsChars(parts []*llm.Part) int {
	count := 0
	for _, p := range parts {
		count += c.estimatePartChars(p)
	}
	return count
}

// countToolDeclarationOverhead returns the estimated token overhead
// for all registered tool declarations. Returns 0 when registry is nil.
func (c *HeuristicTokenCounter) countToolDeclarationOverhead() int {
	if c.registry == nil {
		return 0
	}
	overhead := 0
	for _, decl := range c.registry.GetDeclarations() {
		overhead += (len(decl.Name) + len(decl.Description)) / 4
		if decl.Parameters != nil {
			overhead += 50 // Heuristic for parameter definitions
		}
	}
	return overhead
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
		charCount += estimateMapSizeInternal(p.FunctionCall.Args, 0)
	}
	if p.FunctionResponse != nil {
		charCount += len(p.FunctionResponse.Name)
		charCount += estimateMapSizeInternal(p.FunctionResponse.Response, 0)
	}
	if p.InlineData != nil {
		charCount += 160 // Heuristic for blob (roughly 50 tokens)
	}
	return charCount
}

func estimateMapSizeInternal(m map[string]interface{}, depth int) int {
	if depth > maxEstimateDepth {
		return 0
	}
	if m == nil {
		return 0
	}
	size := 0
	for k, v := range m {
		size += len(k)
		size += estimateValueSizeInternal(v, depth+1)
	}
	return size
}

// estimateMapValueSize delegates map sizing to estimateMapSizeInternal
// with an incremented depth counter.
func estimateMapValueSize(m map[string]interface{}, depth int) int {
	return estimateMapSizeInternal(m, depth+1)
}

// estimateSliceValueSize sums the estimated sizes of each element in
// a slice, plus 1 for the slice structure itself. Recursion depth is
// incremented per element.
func estimateSliceValueSize(s []interface{}, depth int) int {
	size := 1
	for _, item := range s {
		size += estimateValueSizeInternal(item, depth+1)
	}
	return size
}

// stringValueSize returns len(s) as the character-equivalent estimate
// for a string value.
func stringValueSize(s string) int { return len(s) }

// estimateValueSizeInternal guards against excessive recursion depth
// and delegates type-specific estimation to estimateValueSizeUnguarded.
func estimateValueSizeInternal(v interface{}, depth int) int {
	if depth > maxEstimateDepth {
		return 0
	}
	return estimateValueSizeUnguarded(v, depth)
}

// estimateValueSizeUnguarded dispatches to type-specific estimators.
// Nil is handled as a first-class case in the type switch.
func estimateValueSizeUnguarded(v interface{}, depth int) int {
	switch val := v.(type) {
	case nil:
		return nilValueSize
	case string:
		return stringValueSize(val)
	case float64, int, int64:
		return numericValueSize
	case bool:
		return boolValueSize
	case map[string]interface{}:
		return estimateMapValueSize(val, depth)
	case []interface{}:
		return estimateSliceValueSize(val, depth)
	default:
		return unknownValueSize
	}
}
