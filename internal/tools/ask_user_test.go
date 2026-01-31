// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"strings"
	"testing"
)

func TestAskUser_EOF(t *testing.T) {
	sm := NewSecurityManager()
	// Mock Stdin with an empty reader to trigger immediate EOF
	sm.SetInputReader(strings.NewReader(""))

	m := &systemManager{sm: sm}
	ctx := context.Background()
	args := map[string]interface{}{
		"question": "What is your name?",
	}

	res, err := m.askUser(ctx, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "User closed input (EOF)."
	if res.Text != expected {
		t.Errorf("expected %q, got %q", expected, res.Text)
	}
}

func TestAskUser_Success(t *testing.T) {
	sm := NewSecurityManager()
	sm.SetInputReader(strings.NewReader("Alice\n"))

	m := &systemManager{sm: sm}
	ctx := context.Background()
	args := map[string]interface{}{
		"question": "What is your name?",
	}

	res, err := m.askUser(ctx, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Alice"
	if res.Text != expected {
		t.Errorf("expected %q, got %q", expected, res.Text)
	}
}

func TestAskUser_MultipleCalls(t *testing.T) {
	sm := NewSecurityManager()
	// Provide multiple lines to simulate multiple answers
	sm.SetInputReader(strings.NewReader("Alice\nBob\n"))

	m := &systemManager{sm: sm}
	ctx := context.Background()

	// First call
	res1, err := m.askUser(ctx, map[string]interface{}{"question": "Q1"})
	if err != nil || res1.Text != "Alice" {
		t.Fatalf("first call failed: res=%v, err=%v", res1, err)
	}

	// Second call - should get the second line from the same shared reader
	res2, err := m.askUser(ctx, map[string]interface{}{"question": "Q2"})
	if err != nil || res2.Text != "Bob" {
		t.Fatalf("second call failed: res=%v, err=%v", res2, err)
	}
}
