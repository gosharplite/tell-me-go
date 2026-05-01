// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"errors"
	"testing"

	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/require"
)

// setPinnedFailingHM wraps a MockHistoryManager and overrides SetPinned to return an error.
type setPinnedFailingHM struct {
	ports.HistoryManager
	err error
}

func (m *setPinnedFailingHM) SetPinned(ctx context.Context, turnIndex int, pinned bool) error {
	return m.err
}

func TestManageHistory_SetPinnedError(t *testing.T) {
	failingHM := &setPinnedFailingHM{
		HistoryManager: &failingHMBase{},
		err:            errors.New("set pinned failed"),
	}

	cm := &sessctx.Manager{History: failingHM}
	tools := NewInternalTools(cm)

	args := map[string]interface{}{
		"action": "pin",
		"index":  float64(0),
	}

	result, err := tools.ManageHistory(context.Background(), args, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "set pinned failed")
	require.Empty(t, result.Text)
}

func TestManageHistory_SetPinnedError_Unpin(t *testing.T) {
	failingHM := &setPinnedFailingHM{
		HistoryManager: &failingHMBase{},
		err:            errors.New("set pinned failed"),
	}

	cm := &sessctx.Manager{History: failingHM}
	tools := NewInternalTools(cm)

	args := map[string]interface{}{
		"action": "unpin",
		"index":  float64(1),
	}

	result, err := tools.ManageHistory(context.Background(), args, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "set pinned failed")
	require.Empty(t, result.Text)
}

func TestSummarizeHistory_SummarizeRangeError(t *testing.T) {
	// ContextManager without a Summarizer will fail SummarizeRange via validateSummarizer().
	cm := &sessctx.Manager{
		History:  &failingHMBase{},
		Strategy: sessctx.NewStrategy(&mockTokenCounter{}),
	}
	tools := NewInternalTools(cm)

	args := map[string]interface{}{
		"turns": float64(3),
	}

	result, err := tools.SummarizeHistory(context.Background(), args, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrTerminal)
	require.Empty(t, result.Text)
}

// failingHMBase provides the minimal HistoryManager implementation needed for tests.
type failingHMBase struct {
	ports.HistoryManager
}

func (m *failingHMBase) GetTotalEntries() int { return 0 }
func (m *failingHMBase) GetWindow(ctx context.Context, start, end int) ([]*llm.Content, error) {
	return nil, nil
}
func (m *failingHMBase) GetLastUserMessage(ctx context.Context) (string, int, error) {
	return "", 0, nil
}
func (m *failingHMBase) GetResolver() llm.AssetResolver                              { return nil }
func (m *failingHMBase) SetContents(ctx context.Context, c []*llm.Content) error     { return nil }
func (m *failingHMBase) AddContent(ctx context.Context, c *llm.Content) error        { return nil }
func (m *failingHMBase) AppendParts(ctx context.Context, i int, p []*llm.Part) error { return nil }
func (m *failingHMBase) Save(ctx context.Context) error                              { return nil }
func (m *failingHMBase) Sync(ctx context.Context) error                              { return nil }
func (m *failingHMBase) Archive(ctx context.Context, c []*llm.Content) error         { return nil }
func (m *failingHMBase) SetPinned(ctx context.Context, i int, p bool) error          { return nil }
func (m *failingHMBase) GetFilePath() string                                         { return "" }
func (m *failingHMBase) RollbackTurns(ctx context.Context, t int) (int, int, int, error) {
	return 0, 0, 0, nil
}

// mockTokenCounter implements llm.TokenCounter for testing.
type mockTokenCounter struct{}

func (m *mockTokenCounter) Count(contents []*llm.Content) int { return 10 }

// mockToolRegistrar implements tools.ToolRegistrar for testing RegisterInternal error paths.
type mockToolRegistrar struct {
	registerWithOptionsErr error
	registerErr            error
}

func (m *mockToolRegistrar) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.registerErr
}

func (m *mockToolRegistrar) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.registerWithOptionsErr
}

func (m *mockToolRegistrar) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return nil
}

func (m *mockToolRegistrar) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}

func TestRegisterInternal_RegisterWithOptionsError(t *testing.T) {
	reg := &mockToolRegistrar{
		registerWithOptionsErr: errors.New("register with options failed"),
	}

	err := RegisterInternal(reg, &sessctx.Manager{History: &failingHMBase{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "register with options failed")
}

func TestRegisterInternal_RegisterError(t *testing.T) {
	reg := &mockToolRegistrar{
		registerErr: errors.New("register failed"),
	}

	err := RegisterInternal(reg, &sessctx.Manager{History: &failingHMBase{}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "register failed")
}
