// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/require"
)

// panicHook is a heartbeatHooks mock that panics on every tick.
type panicHook struct{}

func (panicHook) onTick() { panic("injected test panic") }

// signalHook is a heartbeatHooks mock that signals a channel on every tick.
// It is idempotent — only the first tick is signaled.
type signalHook struct{ ch chan struct{} }

func (s signalHook) onTick() {
	select {
	case s.ch <- struct{}{}:
	default:
	}
}

// mockLogger is a test spy that captures Error calls for assertion.
type mockLogger struct {
	ports.NoOpLogger // safe default for unused log methods
	errors           []string
}

func (m *mockLogger) Error(msg string, args ...any) {
	m.errors = append(m.errors, fmt.Sprintf(msg, args...))
}

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
	tools := NewInternalTools(cm, &ports.NoOpLogger{})

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
	tools := NewInternalTools(cm, &ports.NoOpLogger{})

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
	tools := NewInternalTools(cm, &ports.NoOpLogger{})

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

	err := RegisterInternal(reg, &sessctx.Manager{History: &failingHMBase{}}, &ports.NoOpLogger{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "register with options failed")
}

func TestRegisterInternal_RegisterError(t *testing.T) {
	reg := &mockToolRegistrar{
		registerErr: errors.New("register failed"),
	}

	err := RegisterInternal(reg, &sessctx.Manager{History: &failingHMBase{}}, &ports.NoOpLogger{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "register failed")
}

func TestRegisterInternal_Success(t *testing.T) {
	reg := &mockToolRegistrar{} // zero value: both error fields nil → both registrations succeed
	err := RegisterInternal(reg, &sessctx.Manager{History: &failingHMBase{}}, &ports.NoOpLogger{})
	require.NoError(t, err)
}

func TestManageHistory_NegativeIndex(t *testing.T) {
	cm := &sessctx.Manager{History: &failingHMBase{}}
	it := NewInternalTools(cm, &ports.NoOpLogger{})

	args := map[string]interface{}{
		"action": "pin",
		"index":  float64(-1),
	}

	result, err := it.ManageHistory(context.Background(), args, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be >= 0")
	require.Empty(t, result.Text)
}

func TestEmitHeartbeats(t *testing.T) {
	t.Run("returns when done is closed before first tick", func(t *testing.T) {
		done := make(chan struct{})
		close(done)
		hb := make(chan struct{})

		it := NewInternalTools(&sessctx.Manager{History: &failingHMBase{}}, &ports.NoOpLogger{})

		returned := make(chan struct{})
		go func() {
			it.emitHeartbeats(done, hb)
			close(returned)
		}()

		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Fatal("emitHeartbeats did not return within 1s")
		}
	})

	t.Run("does not panic with nil heartbeat channel", func(t *testing.T) {
		done := make(chan struct{})
		close(done)

		it := NewInternalTools(&sessctx.Manager{History: &failingHMBase{}}, &ports.NoOpLogger{})

		// Should return cleanly without panic
		it.emitHeartbeats(done, nil)
	})

	t.Run("does not block when heartbeat channel is full", func(t *testing.T) {
		done := make(chan struct{})
		hb := make(chan struct{}) // unbuffered, no consumer
		close(done)

		it := NewInternalTools(&sessctx.Manager{History: &failingHMBase{}}, &ports.NoOpLogger{})

		returned := make(chan struct{})
		go func() {
			it.emitHeartbeats(done, hb)
			close(returned)
		}()

		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Fatal("emitHeartbeats blocked on full channel")
		}
	})
}

func TestSummarizeHistory_UnmarshalArgsError(t *testing.T) {
	cm := &sessctx.Manager{History: &failingHMBase{}}
	it := NewInternalTools(cm, &ports.NoOpLogger{})

	args := map[string]interface{}{
		"turns": "not-a-number",
	}

	result, err := it.SummarizeHistory(context.Background(), args, nil)
	require.Error(t, err)
	require.Empty(t, result.Text)
}

func TestSummarizeHistory_InvalidTurns(t *testing.T) {
	tests := []struct {
		name  string
		turns float64
	}{
		{"zero turns", 0},
		{"negative turns", -5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &sessctx.Manager{History: &failingHMBase{}}
			it := NewInternalTools(cm, &ports.NoOpLogger{})

			args := map[string]interface{}{
				"turns": tt.turns,
			}

			result, err := it.SummarizeHistory(context.Background(), args, nil)
			require.Error(t, err)
			require.Contains(t, err.Error(), "must be > 0")
			require.Empty(t, result.Text)
		})
	}
}

func TestSummarizeHistory_LargeTurns(t *testing.T) {
	cm := &sessctx.Manager{History: &failingHMBase{}}
	it := NewInternalTools(cm, &ports.NoOpLogger{})

	args := map[string]interface{}{
		"turns": float64(1e20),
	}

	result, err := it.SummarizeHistory(context.Background(), args, nil)
	require.Error(t, err)
	require.Empty(t, result.Text)
}

func TestSummarizeHistory_Success(t *testing.T) {
	counter := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(counter)
	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	})

	cm := sessctx.NewManager(strategy, history, nil, nil)
	mockSum := &agenttest.MockSummarizer{}
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "range summary", &llm.Metrics{PromptTokens: 5}, nil
	})
	cm.Summarizer = mockSum

	it := NewInternalTools(cm, &ports.NoOpLogger{})

	args := map[string]interface{}{
		"turns": float64(1),
		"focus": "test focus",
	}

	result, err := it.SummarizeHistory(context.Background(), args, nil)
	require.NoError(t, err)
	require.Contains(t, result.Text, "summarized the first 1 turns")
	require.NotNil(t, result.Metadata)
	require.Contains(t, result.Metadata, "metrics")
}

func TestManageHistory_UnsupportedAction(t *testing.T) {
	cm := &sessctx.Manager{History: &failingHMBase{}}
	it := NewInternalTools(cm, &ports.NoOpLogger{})

	args := map[string]interface{}{
		"action": "delete",
		"index":  float64(0),
	}

	result, err := it.ManageHistory(context.Background(), args, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported action")
	require.Empty(t, result.Text)
}

func TestManageHistory_OutOfBoundsIndex(t *testing.T) {
	failingHM := &setPinnedFailingHM{
		HistoryManager: &failingHMBase{},
		err:            errors.New("index out of range"),
	}
	cm := &sessctx.Manager{History: failingHM}
	it := NewInternalTools(cm, &ports.NoOpLogger{})

	args := map[string]interface{}{
		"action": "pin",
		"index":  float64(99999),
	}

	result, err := it.ManageHistory(context.Background(), args, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "index out of range")
	require.Empty(t, result.Text)
}

func TestManageHistory_UnmarshalArgsError(t *testing.T) {
	cm := &sessctx.Manager{History: &failingHMBase{}}
	it := NewInternalTools(cm, &ports.NoOpLogger{})

	// Type mismatch: "action" is a number, but params.Action expects a string
	args := map[string]interface{}{
		"action": 123,
		"index":  float64(0),
	}

	result, err := it.ManageHistory(context.Background(), args, nil)
	require.Error(t, err)
	require.Empty(t, result.Text)
}

func TestManageHistory_Success(t *testing.T) {
	cm := &sessctx.Manager{History: &failingHMBase{}}
	it := NewInternalTools(cm, &ports.NoOpLogger{})

	t.Run("pin", func(t *testing.T) {
		args := map[string]interface{}{
			"action": "pin",
			"index":  float64(2),
		}
		result, err := it.ManageHistory(context.Background(), args, nil)
		require.NoError(t, err)
		require.Contains(t, result.Text, "turn 2 has been successfully pinned")
	})

	t.Run("unpin", func(t *testing.T) {
		args := map[string]interface{}{
			"action": "unpin",
			"index":  float64(3),
		}
		result, err := it.ManageHistory(context.Background(), args, nil)
		require.NoError(t, err)
		require.Contains(t, result.Text, "turn 3 has been successfully unpinned")
	})
}

func TestEmitHeartbeats_PanicRecovery(t *testing.T) {
	// 1. Create InternalTools with a mockLogger to capture the Error call.
	logger := &mockLogger{}
	it := NewInternalTools(&sessctx.Manager{History: &failingHMBase{}}, logger)

	// 2. Install the test-only panic hook.
	it.hooks = panicHook{}

	// 3. Start emitHeartbeats in a background goroutine.
	done := make(chan struct{})
	hb := make(chan struct{})

	returned := make(chan struct{})
	go func() {
		it.emitHeartbeats(done, hb)
		close(returned)
	}()

	// 4. Wait for the goroutine to exit. The panic fires on the first tick
	//    (2s interval), recover catches it, and the method returns cleanly.
	select {
	case <-returned:
		// goroutine exited as expected
	case <-time.After(5 * time.Second):
		t.Fatal("emitHeartbeats did not return within 5s — possible goroutine leak")
	}

	// Cleanup: close done channel (no-op since goroutine already exited).
	close(done)

	// 5. Verify the logger captured the panic message.
	require.NotEmpty(t, logger.errors)
	require.Contains(t, logger.errors[0],
		"panic in summarize history background drainer: injected test panic")
}

// waitForTicked blocks until a tick is signaled or the test times out after 5s.
func waitForTicked(t *testing.T, ticked <-chan struct{}) {
	t.Helper()
	select {
	case <-ticked:
	case <-time.After(5 * time.Second):
		t.Fatal("ticker did not fire within 5s")
	}
}

// waitForReturned blocks until emitHeartbeats returns or the test times out after 3s.
func waitForReturned(t *testing.T, returned <-chan struct{}) {
	t.Helper()
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("emitHeartbeats did not return within 3s — possible goroutine leak")
	}
}

// verifyHeartbeat asserts that a heartbeat is immediately receivable on a buffered channel.
func verifyHeartbeat(t *testing.T, hb <-chan struct{}) {
	t.Helper()
	select {
	case <-hb:
	default:
		t.Error("expected heartbeat on buffered channel but none received")
	}
}

// testEmitHeartbeatsPath exercises one behavioral path through emitHeartbeats.
// It creates an InternalTools instance, installs an idempotent tick hook, starts
// emitHeartbeats in a goroutine, waits for the first tick, stops the goroutine,
// verifies clean return, and optionally checks heartbeat delivery.
func testEmitHeartbeatsPath(t *testing.T, hb chan struct{}, wantRecv bool) {
	t.Helper()

	it := NewInternalTools(&sessctx.Manager{History: &failingHMBase{}}, &ports.NoOpLogger{})

	// Install a tick hook that signals once.
	ticked := make(chan struct{}, 1)
	it.hooks = signalHook{ch: ticked}

	done := make(chan struct{})

	returned := make(chan struct{})
	go func() {
		it.emitHeartbeats(done, hb)
		close(returned)
	}()

	// Wait for the first tick to fire.
	waitForTicked(t, ticked)

	// Stop the goroutine.
	close(done)

	// Verify goroutine returns cleanly (no leak, no panic).
	waitForReturned(t, returned)

	// For the "send on capacity" case, verify exactly one heartbeat
	// was delivered through the buffered channel.
	if wantRecv {
		verifyHeartbeat(t, hb)
	}
}

func TestEmitHeartbeats_TickerPaths(t *testing.T) {
	// These subtests set a tick hook on the InternalTools instance and must
	// not run in parallel.

	t.Run("nil hb skip", func(t *testing.T) {
		testEmitHeartbeatsPath(t, nil, false)
	})
	t.Run("full channel drop", func(t *testing.T) {
		testEmitHeartbeatsPath(t, make(chan struct{}), false)
	})
	t.Run("send on capacity", func(t *testing.T) {
		testEmitHeartbeatsPath(t, make(chan struct{}, 1), true)
	})
}

func TestProdHeartbeatHooks_OnTick(t *testing.T) {
	// Verify the production no-op doesn't panic.
	prodHeartbeatHooks{}.onTick()
}

func TestNewInternalTools_NilLogger(t *testing.T) {
	it := NewInternalTools(&sessctx.Manager{History: &failingHMBase{}}, nil)
	require.NotNil(t, it.logger, "nil logger should fall back to NoOpLogger")
	_, ok := it.logger.(*ports.NoOpLogger)
	require.True(t, ok, "nil logger should fall back to *ports.NoOpLogger")
}
