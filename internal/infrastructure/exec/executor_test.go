// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package exec

import (
	"context"
	"strings"
	"testing"
)

func TestRealExecutor(t *testing.T) {
	t.Parallel()
	e := &RealExecutor{}
	ctx := context.Background()

	t.Run("Output", func(t *testing.T) {
		t.Parallel()
		out, err := e.Output(ctx, "echo", "hello")
		if err != nil {
			t.Fatalf("Output failed: %v", err)
		}
		if strings.TrimSpace(string(out)) != "hello" {
			t.Errorf("expected hello, got %q", out)
		}
	})

	t.Run("CombinedOutput", func(t *testing.T) {
		t.Parallel()
		out, err := e.CombinedOutput(ctx, "echo", "world")
		if err != nil {
			t.Fatalf("CombinedOutput failed: %v", err)
		}
		if strings.TrimSpace(string(out)) != "world" {
			t.Errorf("expected world, got %q", out)
		}
	})

	t.Run("Failure", func(t *testing.T) {
		t.Parallel()
		_, err := e.Output(ctx, "nonexistent-command-12345")
		if err == nil {
			t.Error("expected error for nonexistent command, got nil")
		}
	})
}
