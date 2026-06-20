// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestMockCapturer_IsTTY(t *testing.T) {
	t.Parallel()
	var called bool
	m := &MockCapturer{
		IsTTYFn: func(v any) bool {
			called = true
			return v == "val"
		},
	}
	got := m.IsTTY("val")
	if !got {
		t.Error("got false; want true")
	}
	if !called {
		t.Error("IsTTYFn was not called")
	}
}

func TestMockCapturer_CapturePrompt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var called bool
	m := &MockCapturer{
		CapturePromptFn: func(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
			called = true
			return "result", nil
		},
	}
	got, err := m.CapturePrompt(ctx, []string{"a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "result" {
		t.Errorf("got %q; want %q", got, "result")
	}
	if !called {
		t.Error("CapturePromptFn was not called")
	}
}

func TestMockCapturer_Confirm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var called bool
	m := &MockCapturer{
		ConfirmFn: func(ctx context.Context, message string) (bool, error) {
			called = true
			return true, nil
		},
	}
	got, err := m.Confirm(ctx, "msg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("got false; want true")
	}
	if !called {
		t.Error("ConfirmFn was not called")
	}
}

func TestMockCapturer_Close(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var called bool
	m := &MockCapturer{
		CloseFn: func(ctx context.Context) error {
			called = true
			return nil
		},
	}
	err := m.Close(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("CloseFn was not called")
	}
}

func TestMockCapturer_Warn(t *testing.T) {
	t.Parallel()
	var called bool
	m := &MockCapturer{
		WarnFn: func(msg string) {
			called = true
		},
	}
	m.Warn("msg")
	if !called {
		t.Error("WarnFn was not called")
	}
}

func TestMockCapturer_Prompt(t *testing.T) {
	t.Parallel()
	var called bool
	m := &MockCapturer{
		PromptFn: func(msg string) {
			called = true
		},
	}
	m.Prompt("msg")
	if !called {
		t.Error("PromptFn was not called")
	}
}

func TestMockCapturer_ReadSingleKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var called bool
	m := &MockCapturer{
		ReadSingleKeyFn: func(ctx context.Context) (string, error) {
			called = true
			return "a", nil
		},
	}
	got, err := m.ReadSingleKey(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "a" {
		t.Errorf("got %q; want %q", got, "a")
	}
	if !called {
		t.Error("ReadSingleKeyFn was not called")
	}
}

func TestMockCapturer_ReadLine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var called bool
	m := &MockCapturer{
		ReadLineFn: func(ctx context.Context) (string, error) {
			called = true
			return "line", nil
		},
	}
	got, err := m.ReadLine(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "line" {
		t.Errorf("got %q; want %q", got, "line")
	}
	if !called {
		t.Error("ReadLineFn was not called")
	}
}

func TestMockCapturer_RaceDetection(t *testing.T) {
	m := &MockCapturer{}
	ctx := context.Background()

	var (
		isTTYCalls         atomic.Int32
		capturePromptCalls atomic.Int32
		confirmCalls       atomic.Int32
		closeCalls         atomic.Int32
		warnCalls          atomic.Int32
		promptCalls        atomic.Int32
		readSingleKeyCalls atomic.Int32
		readLineCalls      atomic.Int32
	)

	m.IsTTYFn = func(v any) bool { isTTYCalls.Add(1); return false }
	m.CapturePromptFn = func(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
		capturePromptCalls.Add(1)
		return "", nil
	}
	m.ConfirmFn = func(ctx context.Context, message string) (bool, error) {
		confirmCalls.Add(1)
		return false, nil
	}
	m.CloseFn = func(ctx context.Context) error {
		closeCalls.Add(1)
		return nil
	}
	m.WarnFn = func(msg string) { warnCalls.Add(1) }
	m.PromptFn = func(msg string) { promptCalls.Add(1) }
	m.ReadSingleKeyFn = func(ctx context.Context) (string, error) {
		readSingleKeyCalls.Add(1)
		return "", nil
	}
	m.ReadLineFn = func(ctx context.Context) (string, error) {
		readLineCalls.Add(1)
		return "", nil
	}

	var wg sync.WaitGroup
	const goroutines = 5
	const iterations = 20

	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				m.IsTTY("x")
				_, _ = m.CapturePrompt(ctx, nil)
				_, _ = m.Confirm(ctx, "y")
				_ = m.Close(ctx)
				m.Warn("w")
				m.Prompt("p")
				_, _ = m.ReadSingleKey(ctx)
				_, _ = m.ReadLine(ctx)
			}
		}()
	}
	wg.Wait()

	it := int(isTTYCalls.Load())
	cp := int(capturePromptCalls.Load())
	cf := int(confirmCalls.Load())
	cl := int(closeCalls.Load())
	wc := int(warnCalls.Load())
	pc := int(promptCalls.Load())
	rsk := int(readSingleKeyCalls.Load())
	rl := int(readLineCalls.Load())

	want := goroutines * iterations
	total := it + cp + cf + cl + wc + pc + rsk + rl
	wantTotal := want * 8
	if total != wantTotal {
		t.Errorf("got total %d (counts [%d,%d,%d,%d,%d,%d,%d,%d]); want %d",
			total, it, cp, cf, cl, wc, pc, rsk, rl, wantTotal)
	}
}

// assertIsTTYNilReturnsFalse asserts that a zero-value MockCapturer
// returns false from IsTTY (the nil-func default).
func assertIsTTYNilReturnsFalse(t *testing.T, m *MockCapturer) {
	t.Helper()
	got := m.IsTTY("any")
	if got {
		t.Error("nil IsTTYFn: got true; want false")
	}
}

// assertCapturePromptNilReturnsEmpty asserts that a zero-value MockCapturer
// returns ("", nil) from CapturePrompt (the nil-func default).
func assertCapturePromptNilReturnsEmpty(t *testing.T, m *MockCapturer) {
	t.Helper()
	ctx := context.Background()
	got, err := m.CapturePrompt(ctx, nil)
	if got != "" {
		t.Errorf("nil CapturePromptFn: got %q; want empty", got)
	}
	if err != nil {
		t.Errorf("nil CapturePromptFn: unexpected error: %v", err)
	}
}

// assertConfirmNilReturnsFalse asserts that a zero-value MockCapturer
// returns (false, nil) from Confirm (the nil-func default).
func assertConfirmNilReturnsFalse(t *testing.T, m *MockCapturer) {
	t.Helper()
	ctx := context.Background()
	got, err := m.Confirm(ctx, "proceed?")
	if got {
		t.Error("nil ConfirmFn: got true; want false")
	}
	if err != nil {
		t.Errorf("nil ConfirmFn: unexpected error: %v", err)
	}
}

// assertCloseNilReturnsNil asserts that a zero-value MockCapturer
// returns nil from Close (the nil-func default).
func assertCloseNilReturnsNil(t *testing.T, m *MockCapturer) {
	t.Helper()
	ctx := context.Background()
	err := m.Close(ctx)
	if err != nil {
		t.Errorf("nil CloseFn: unexpected error: %v", err)
	}
}

// assertReadSingleKeyNilReturnsEmpty asserts that a zero-value MockCapturer
// returns ("", nil) from ReadSingleKey (the nil-func default).
func assertReadSingleKeyNilReturnsEmpty(t *testing.T, m *MockCapturer) {
	t.Helper()
	ctx := context.Background()
	got, err := m.ReadSingleKey(ctx)
	if got != "" {
		t.Errorf("nil ReadSingleKeyFn: got %q; want empty", got)
	}
	if err != nil {
		t.Errorf("nil ReadSingleKeyFn: unexpected error: %v", err)
	}
}

// assertReadLineNilReturnsEmpty asserts that a zero-value MockCapturer
// returns ("", nil) from ReadLine (the nil-func default).
func assertReadLineNilReturnsEmpty(t *testing.T, m *MockCapturer) {
	t.Helper()
	ctx := context.Background()
	got, err := m.ReadLine(ctx)
	if got != "" {
		t.Errorf("nil ReadLineFn: got %q; want empty", got)
	}
	if err != nil {
		t.Errorf("nil ReadLineFn: unexpected error: %v", err)
	}
}

func TestMockCapturer_NilFuncs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(m *MockCapturer)
	}{
		{
			name: "IsTTY_nil_func",
			call: func(m *MockCapturer) {
				assertIsTTYNilReturnsFalse(t, m)
			},
		},
		{
			name: "CapturePrompt_nil_func",
			call: func(m *MockCapturer) {
				assertCapturePromptNilReturnsEmpty(t, m)
			},
		},
		{
			name: "Confirm_nil_func",
			call: func(m *MockCapturer) {
				assertConfirmNilReturnsFalse(t, m)
			},
		},
		{
			name: "Close_nil_func",
			call: func(m *MockCapturer) {
				assertCloseNilReturnsNil(t, m)
			},
		},
		{
			name: "ReadSingleKey_nil_func",
			call: func(m *MockCapturer) {
				assertReadSingleKeyNilReturnsEmpty(t, m)
			},
		},
		{
			name: "ReadLine_nil_func",
			call: func(m *MockCapturer) {
				assertReadLineNilReturnsEmpty(t, m)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &MockCapturer{} // zero value — no Fn fields set
			tt.call(m)
		})
	}
}
