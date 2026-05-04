// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"strings"
	"testing"

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
