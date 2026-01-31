package tools

import (
	"context"
	"os"
	"testing"
)

func TestConfirmDestructiveAction_Cancellation(t *testing.T) {
	sm := NewSecurityManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// This should return an error immediately because the context is canceled
	_, err := sm.ConfirmDestructiveAction(ctx, "test action", "test target", "test detail")
	if err == nil {
		t.Error("expected error for canceled context, got nil")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestReadSingleKey_MockCancellation(t *testing.T) {
	sm := NewSecurityManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sm.readSingleKey(ctx)
	if err == nil {
		t.Error("expected error for canceled context, got nil")
	}
}

func TestReadSingleKey_MockInterruption(t *testing.T) {
	sm := NewSecurityManager()
	// We can't easily mock Stdin for readSingleKey without more refactoring,
	// but we can at least test the mock answer path.
	os.Setenv("TELL_ME_MOCK_ANSWER", "y")
	defer os.Unsetenv("TELL_ME_MOCK_ANSWER")

	char, err := sm.readSingleKey(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if char != "y" {
		t.Errorf("expected 'y', got %q", char)
	}
}
