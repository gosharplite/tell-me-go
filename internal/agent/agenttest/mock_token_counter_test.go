// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestMockTokenCounter_Count(t *testing.T) {
	t.Parallel()

	m := &MockTokenCounter{Tokens: 42}
	got := m.Count([]*llm.Content{{Role: "user"}})
	if got != 42 {
		t.Errorf("got %d; want 42", got)
	}
}

func TestMockTokenCounter_Count_Zero(t *testing.T) {
	t.Parallel()

	m := &MockTokenCounter{} // Tokens defaults to 0
	got := m.Count(nil)
	if got != 0 {
		t.Errorf("got %d; want 0", got)
	}
}

func TestMockTokenCounter_SetTokens(t *testing.T) {
	t.Parallel()

	m := &MockTokenCounter{}
	m.SetTokens(99)
	if m.Tokens != 99 {
		t.Errorf("got Tokens %d; want 99", m.Tokens)
	}

	got := m.Count(nil)
	if got != 99 {
		t.Errorf("got %d; want 99", got)
	}
}

func TestMockTokenCounter_EstimateTokens(t *testing.T) {
	t.Parallel()

	m := &MockTokenCounter{Tokens: 7}
	got := m.EstimateTokens([]*llm.Content{{Role: "assistant"}})
	if got != 7 {
		t.Errorf("got %d; want 7", got)
	}
}
