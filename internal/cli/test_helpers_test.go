// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// mockCapturerInteractor implements agent.CapturerInteractor for testing
// without a real TTY. IsTTY returns a configurable value.
var _ agent.CapturerInteractor = (*mockCapturerInteractor)(nil)

type mockCapturerInteractor struct {
	isTTY   bool
	closeFn func(stdctx.Context) error
}

func (m *mockCapturerInteractor) IsTTY(v any) bool { return m.isTTY }
func (m *mockCapturerInteractor) CapturePrompt(ctx stdctx.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	return "", nil
}
func (m *mockCapturerInteractor) Confirm(ctx stdctx.Context, msg string) (bool, error) { return true, nil }
func (m *mockCapturerInteractor) Warn(msg string)                                      {}
func (m *mockCapturerInteractor) Prompt(msg string)                                    {}
func (m *mockCapturerInteractor) ReadLine(ctx stdctx.Context) (string, error)          { return "", nil }
func (m *mockCapturerInteractor) ReadSingleKey(ctx stdctx.Context) (string, error)     { return "", nil }
func (m *mockCapturerInteractor) Close(ctx stdctx.Context) error {
	if m.closeFn != nil {
		return m.closeFn(ctx)
	}
	return nil
}

// stubHistoryManager satisfies ports.HistoryManager with no-op methods.
// It is used when tests need to pass a non-nil HistoryManager through
// mockBootstrapper without requiring real behavior.
type stubHistoryManager struct{}

func (s *stubHistoryManager) GetWindow(_ stdctx.Context, _, _ int) ([]*llm.Content, error) {
	return nil, nil
}
func (s *stubHistoryManager) GetTotalEntries() int { return 0 }
func (s *stubHistoryManager) GetLastUserMessage(_ stdctx.Context) (string, int, error) {
	return "", 0, nil
}
func (s *stubHistoryManager) GetResolver() llm.AssetResolver                           { return nil }
func (s *stubHistoryManager) SetContents(_ stdctx.Context, _ []*llm.Content) error     { return nil }
func (s *stubHistoryManager) AddContent(_ stdctx.Context, _ *llm.Content) error        { return nil }
func (s *stubHistoryManager) AppendParts(_ stdctx.Context, _ int, _ []*llm.Part) error { return nil }
func (s *stubHistoryManager) Save(_ stdctx.Context) error                              { return nil }
func (s *stubHistoryManager) Sync(_ stdctx.Context) error                              { return nil }
func (s *stubHistoryManager) Archive(_ stdctx.Context, _ []*llm.Content) error         { return nil }
func (s *stubHistoryManager) SetPinned(_ stdctx.Context, _ int, _ bool) error          { return nil }
func (s *stubHistoryManager) GetFilePath() string                                      { return "" }
func (s *stubHistoryManager) RollbackTurns(_ stdctx.Context, _ int) (int, int, int, error) {
	return 0, 0, 0, nil
}

// stubUnifiedHistoryProvider satisfies ports.UnifiedHistoryProvider.
type stubUnifiedHistoryProvider struct{}

func (s *stubUnifiedHistoryProvider) GetHistoryStream(_ stdctx.Context, _ int, _ string) ([]ports.HistoryViewDTO, string, error) {
	return nil, "", nil
}
