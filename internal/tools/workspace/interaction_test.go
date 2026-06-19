// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

func TestInteractionTool(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	it := newinteractionTool(sm)
	ctx := context.Background()

	t.Run("Ask User", func(t *testing.T) {
		sm.Interactor = &toolstest.MockInteractor{Answer: "The answer is 42\n"}
		res, err := it.askUser(ctx, map[string]interface{}{
			"question": "What is the answer?",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.Text, "The answer is 42") {
			t.Errorf("unexpected response: %s", res.Text)
		}
	})

	t.Run("Ask User EOF", func(t *testing.T) {
		sm.Interactor = &toolstest.MockInteractor{Answer: ""} // Immediate EOF
		res, err := it.askUser(ctx, map[string]interface{}{
			"question": "What is the answer?",
		}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if res.Text != "User closed input (EOF)." {
			t.Errorf("expected EOF message, got %s", res.Text)
		}
	})

	t.Run("Ask User Read Error", func(t *testing.T) {
		sm.Interactor = &toolstest.MockInteractor{Err: context.DeadlineExceeded}
		_, err := it.askUser(ctx, map[string]interface{}{
			"question": "What is the answer?",
		}, nil)
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to read user response") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Missing Question", func(t *testing.T) {
		_, err := it.askUser(ctx, map[string]interface{}{}, nil)
		if err == nil {
			t.Error("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "question argument is required") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("Invalid Args", func(t *testing.T) {
		_, err := it.askUser(ctx, map[string]interface{}{
			"question": 123,
		}, nil)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// askUser heartbeat path
// ---------------------------------------------------------------------------

func TestAskUser_WithHeartbeat(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.Interactor = &toolstest.MockInteractor{Answer: "yes\n"}
	it := newinteractionTool(sm)
	ctx := context.Background()
	hb := make(chan struct{}, 2)
	res, err := it.askUser(ctx, map[string]interface{}{
		"question": "Proceed?",
	}, hb)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "yes") {
		t.Errorf("expected 'yes', got: %s", res.Text)
	}
}

// TestInteractionTool_askUser verifies that askUser wraps a non-EOF ReadLine
// error with the "failed to read user response" prefix.
func TestInteractionTool_askUser(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.Interactor = &toolstest.MockInteractor{Err: context.DeadlineExceeded}
	it := newinteractionTool(sm)
	ctx := context.Background()

	_, err := it.askUser(ctx, map[string]interface{}{
		"question": "What is the answer?",
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read user response") {
		t.Errorf("expected 'failed to read user response' in error, got: %v", err)
	}
}

// blockingTerminalController is a test double for domain_security.TerminalController
// that blocks ReadLine until signalled, allowing the askUser heartbeat ticker to fire.
type blockingTerminalController struct {
	toolstest.MockSecurityManager
	unblock chan struct{}
}

func (b *blockingTerminalController) ReadLine(ctx context.Context) (string, error) {
	select {
	case <-b.unblock:
		return "yes\n", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// TestAskUser_HeartbeatTickerFires ensures the ticker.C branch inside askUser's
// heartbeat goroutine is exercised. The ReadLine mock blocks until the test
// receives a heartbeat, guaranteeing the 2-second ticker fires.
func TestAskUser_HeartbeatTickerFires(t *testing.T) {
	unblock := make(chan struct{})

	sm := &blockingTerminalController{
		MockSecurityManager: toolstest.MockSecurityManager{AllowAll: true},
		unblock:             unblock,
	}
	it := newinteractionTool(sm)
	ctx := context.Background()
	hb := make(chan struct{}, 1)

	type result struct {
		res tools.ToolResult
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := it.askUser(ctx, map[string]interface{}{
			"question": "Proceed?",
		}, hb)
		done <- result{res, err}
	}()

	// Wait for the ticker to fire and deliver a heartbeat.
	select {
	case <-hb:
		// Heartbeat received — the case <-ticker.C branch is now covered.
		close(unblock)
	case <-done:
		t.Fatal("askUser returned before ticker fired")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for heartbeat ticker to fire")
	}

	// Collect result.
	r := <-done
	if r.err != nil {
		t.Fatalf("unexpected error: %v", r.err)
	}
	if !strings.Contains(r.res.Text, "yes") {
		t.Errorf("expected 'yes', got: %s", r.res.Text)
	}
}
