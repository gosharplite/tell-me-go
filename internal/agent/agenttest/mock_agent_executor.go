// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// MockAgentExecutor is a test double for the agent executor used by the
// TurnEngine. The default Execute returns (nil, nil); override
// ExecuteFunc to script tool-handling behaviour for engine tests.
type MockAgentExecutor struct {
	ExecuteFunc func(ctx context.Context, respContent *llm.Content, Turn int, maxToolTurns int) (*llm.Content, error)
}

func (m *MockAgentExecutor) Execute(ctx context.Context, respContent *llm.Content, Turn int, maxToolTurns int) (*llm.Content, error) {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, respContent, Turn, maxToolTurns)
	}
	return nil, nil
}
