// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// MockEstimator is a test double for the tokenEstimator interface used
// by the TokenGatekeeper. It always returns the value set via
// SetTokens, regardless of the contents passed in. Implements both
// EstimateTokens and Count so it satisfies the broader llm.TokenCounter
// interface as well.
type MockEstimator struct {
	tokens int
}

func (m *MockEstimator) EstimateTokens(contents []*llm.Content) int {
	return m.tokens
}

func (m *MockEstimator) SetTokens(n int) {
	m.tokens = n
}

// Count satisfies the llm.TokenCounter interface; it returns the same
// value as EstimateTokens.
func (m *MockEstimator) Count(contents []*llm.Content) int {
	return m.tokens
}
