// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"strings"
	"testing"
)

func TestMockLogger_ExecutionTimedOut_WithCriticalLogs(t *testing.T) {
	t.Parallel()

	ch := make(chan string, 1)
	m := &MockLogger{CriticalLogs: ch}
	m.ExecutionTimedOut("tool-1")

	select {
	case msg := <-ch:
		if !strings.HasPrefix(msg, "CRITICAL") {
			t.Errorf("got message %q; want CRITICAL prefix", msg)
		}
		if !strings.Contains(msg, "tool-1") {
			t.Errorf("got message %q; want to contain 'tool-1'", msg)
		}
	default:
		t.Fatal("expected CRITICAL message on channel, got none")
	}
}

func TestMockLogger_ExecutionTimedOut_NilCriticalLogs(t *testing.T) {
	t.Parallel()

	m := &MockLogger{CriticalLogs: nil}
	// Must not panic.
	m.ExecutionTimedOut("tool-2")
}

func TestMockLogger_ExecutionCompletedLate_NoOp(t *testing.T) {
	t.Parallel()

	m := &MockLogger{}
	// Must not panic.
	m.ExecutionCompletedLate("tool-3")
}
