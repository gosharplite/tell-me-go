// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
)

func TestMockCapturer_IsTTY(t *testing.T) {
	t.Parallel()

	m := new(MockCapturer)
	m.On("IsTTY", "val").Return(true)

	got := m.IsTTY("val")
	if !got {
		t.Error("got false; want true")
	}
	m.AssertExpectations(t)
}

func TestMockCapturer_CapturePrompt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	m := new(MockCapturer)
	m.On("CapturePrompt", ctx, []string{"a"}, mock.Anything).Return("result", nil)

	got, err := m.CapturePrompt(ctx, []string{"a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "result" {
		t.Errorf("got %q; want %q", got, "result")
	}
	m.AssertExpectations(t)
}

func TestMockCapturer_Confirm(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	m := new(MockCapturer)
	m.On("Confirm", ctx, "msg").Return(true, nil)

	got, err := m.Confirm(ctx, "msg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("got false; want true")
	}
	m.AssertExpectations(t)
}

func TestMockCapturer_Close(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	m := new(MockCapturer)
	m.On("Close", ctx).Return(nil)

	err := m.Close(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockCapturer_Warn(t *testing.T) {
	t.Parallel()

	m := new(MockCapturer)
	m.On("Warn", "msg").Return()

	m.Warn("msg")
	m.AssertCalled(t, "Warn", "msg")
}

func TestMockCapturer_Prompt(t *testing.T) {
	t.Parallel()

	m := new(MockCapturer)
	m.On("Prompt", "msg").Return()

	m.Prompt("msg")
	m.AssertCalled(t, "Prompt", "msg")
}

func TestMockCapturer_ReadSingleKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	m := new(MockCapturer)
	m.On("ReadSingleKey", ctx).Return("a", nil)

	got, err := m.ReadSingleKey(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "a" {
		t.Errorf("got %q; want %q", got, "a")
	}
	m.AssertExpectations(t)
}

func TestMockCapturer_ReadLine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	m := new(MockCapturer)
	m.On("ReadLine", ctx).Return("line", nil)

	got, err := m.ReadLine(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "line" {
		t.Errorf("got %q; want %q", got, "line")
	}
	m.AssertExpectations(t)
}
