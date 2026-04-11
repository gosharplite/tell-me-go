// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestShellTool_ExecuteCommand_Structured(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	tool := newshellTool(sm, security.NewCommandValidator(sm, nil))
	ctx := context.Background()

	t.Run("Structured args (no shell)", func(t *testing.T) {
		args := map[string]interface{}{
			"args":   []interface{}{helperPath, "echo", "hello", "world"},
			"reason": "testing structured args",
		}
		res, err := tool.ExecuteCommand(ctx, args, nil)
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}
		if !strings.Contains(res.Text, "hello world") {
			t.Errorf("expected output to contain 'hello world', got %q", res.Text)
		}
	})

	t.Run("Structured args with spaces (no quoting hell)", func(t *testing.T) {
		args := map[string]interface{}{
			"args":   []interface{}{helperPath, "echo", "hello world"},
			"reason": "testing structured args with spaces",
		}
		res, err := tool.ExecuteCommand(ctx, args, nil)
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}
		// If it correctly passes "hello world" as a single argument to the helper's 'echo'
		// the helper should print exactly "hello world\n".
		if !strings.Contains(res.Text, "hello world") {
			t.Errorf("expected output to contain 'hello world', got %q", res.Text)
		}
	})

	t.Run("Environment variables in ExecuteCommand", func(t *testing.T) {
		// helper printenv <VAR> prints the value of the environment variable.
		args := map[string]interface{}{
			"args":   []interface{}{helperPath, "printenv", "MY_TEST_VAR"},
			"env":    map[string]interface{}{"MY_TEST_VAR": "STILL_WORKING"},
			"reason": "testing env vars",
		}
		res, err := tool.ExecuteCommand(ctx, args, nil)
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}
		if !strings.Contains(res.Text, "STILL_WORKING") {
			t.Errorf("expected output to contain 'STILL_WORKING', got %q", res.Text)
		}
	})
}
