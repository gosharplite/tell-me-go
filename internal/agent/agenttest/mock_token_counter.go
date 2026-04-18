// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// MockTokenCounter is a test double for llm.TokenCounter. It always
// returns the value held in Tokens, regardless of the contents passed
// in, allowing tests to script a fixed token count.
type MockTokenCounter struct {
	Tokens int
}

func (m *MockTokenCounter) Count(contents []*llm.Content) int {
	return m.Tokens
}

func (m *MockTokenCounter) SetTokens(n int) {
	m.Tokens = n
}

// EstimateTokens is an alias for Count, matching the signature of the
// alternative tokenEstimator interface.
func (m *MockTokenCounter) EstimateTokens(contents []*llm.Content) int {
	return m.Count(contents)
}
