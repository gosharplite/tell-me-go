// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

// TokenCounter defines the interface for calculating token usage.
type TokenCounter interface {
	// Count returns the estimated number of tokens for the given contents.
	Count(contents []*Content) int
	// CountTokens returns the estimated number of tokens for a plain text string.
	CountTokens(text string) int
}

// TokenEstimator defines the interface for token counting.
type TokenEstimator interface {
	EstimateTokens(contents []*Content) int
}
