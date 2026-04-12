// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Exported for external tests
type MockSecurityManager = mockSecurityManager
type MockConsentSecurityManager = mockConsentSecurityManager
type PanicRegistry = panicRegistry
type MockEventBus = mockEventBus
type MockLogger = mockLogger
type ExecutorOption = executorOption

func WithToolTimeout(timeout time.Duration) ExecutorOption {
	return withToolTimeout(timeout)
}

func (e *Dispatcher) AssembleResponse(calls []*llm.FunctionCall, results []tools.ToolResult) *llm.Content {
	return e.assembleResponse(calls, results)
}

func SuggestTool(hallucinated string, validTools []string) string {
	return suggestTool(hallucinated, validTools)
}

const CISafeTimeout = ciSafeTimeout
