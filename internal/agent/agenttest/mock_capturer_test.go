// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestMockCapturer_IsTTY(t *testing.T) {
	t.Parallel()
	m := &MockCapturer{
		IsTTYFn: func(v any) bool { return v == "val" },
	}
	got := m.IsTTY("val")
	if !got {
		t.Error("got false; want true")
	}
	it, _, _, _, _, _, _, _, _ := m.Snapshot()
	if it != 1 {
		t.Errorf("IsTTY calls: got %d, want 1", it)
	}
}

func TestMockCapturer_CapturePrompt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &MockCapturer{
		CapturePromptFn: func(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
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
	_, cp, _, _, _, _, _, _, _ := m.Snapshot()
	if cp != 1 {
		t.Errorf("CapturePrompt calls: got %d, want 1", cp)
	}
}

func TestMockCapturer_Confirm(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &MockCapturer{
		ConfirmFn: func(ctx context.Context, message string) (bool, error) {
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
	_, _, cf, _, _, _, _, _, _ := m.Snapshot()
	if cf != 1 {
		t.Errorf("Confirm calls: got %d, want 1", cf)
	}
}

func TestMockCapturer_Close(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &MockCapturer{
		CloseFn: func(ctx context.Context) error {
			return nil
		},
	}
	err := m.Close(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, _, _, cl, _, _, _, _, _ := m.Snapshot()
	if cl != 1 {
		t.Errorf("Close calls: got %d, want 1", cl)
	}
}

func TestMockCapturer_Warn(t *testing.T) {
	t.Parallel()
	m := &MockCapturer{}
	m.Warn("msg") // nil WarnFn — no-op but still tracked

	_, _, _, _, wc, _, _, _, _ := m.Snapshot()
	if wc != 1 {
		t.Errorf("Warn calls: got %d, want 1", wc)
	}
}

func TestMockCapturer_Prompt(t *testing.T) {
	t.Parallel()
	m := &MockCapturer{}
	m.Prompt("msg") // nil PromptFn — no-op but still tracked

	_, _, _, _, _, pc, _, _, _ := m.Snapshot()
	if pc != 1 {
		t.Errorf("Prompt calls: got %d, want 1", pc)
	}
}

func TestMockCapturer_ReadSingleKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &MockCapturer{
		ReadSingleKeyFn: func(ctx context.Context) (string, error) {
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
	_, _, _, _, _, _, rsk, _, _ := m.Snapshot()
	if rsk != 1 {
		t.Errorf("ReadSingleKey calls: got %d, want 1", rsk)
	}
}

func TestMockCapturer_ReadLine(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m := &MockCapturer{
		ReadLineFn: func(ctx context.Context) (string, error) {
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
	_, _, _, _, _, _, _, rl, _ := m.Snapshot()
	if rl != 1 {
		t.Errorf("ReadLine calls: got %d, want 1", rl)
	}
}

func TestMockCapturer_RaceDetection(t *testing.T) {
	m := &MockCapturer{}
	ctx := context.Background()

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

	it, cp, cf, cl, wc, pc, rsk, rl, _ := m.Snapshot()
	want := goroutines * iterations
	if it != want || cp != want || cf != want || cl != want || wc != want || pc != want || rsk != want || rl != want {
		t.Errorf("got counts [%d,%d,%d,%d,%d,%d,%d,%d]; want [%d x8]",
			it, cp, cf, cl, wc, pc, rsk, rl, want)
	}
}
