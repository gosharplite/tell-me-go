package executor

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// mockToolPipeline is a test double for the ToolPipeline interface.
type mockToolPipeline struct {
	ExecuteToolFunc         func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult
	RequestBatchConsentFunc func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool)
	IsSerialFunc            func(toolName string) bool
}

func (m *mockToolPipeline) ExecuteTool(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
	if m.ExecuteToolFunc != nil {
		return m.ExecuteToolFunc(ctx, call)
	}
	return tools.ToolResult{Text: "mocked execute"}
}

func (m *mockToolPipeline) RequestBatchConsent(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
	if m.RequestBatchConsentFunc != nil {
		return m.RequestBatchConsentFunc(ctx, calls)
	}
	return ctx, make(map[int]bool)
}

func (m *mockToolPipeline) IsSerial(toolName string) bool {
	if m.IsSerialFunc != nil {
		return m.IsSerialFunc(toolName)
	}
	return false
}
