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
		out, err := e.Output(ctx, helperPath, "echo", "hello")
		if err != nil {
			t.Fatalf("Output failed: %v", err)
		}
		if strings.TrimSpace(string(out)) != "hello" {
			t.Errorf("expected hello, got %q", out)
		}
	})

	t.Run("CombinedOutput", func(t *testing.T) {
		t.Parallel()
		out, err := e.CombinedOutput(ctx, helperPath, "echo", "world")
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

func TestRealExecutor_LookPath(t *testing.T) {
	e := &RealExecutor{}

	// Positive case: "go" should exist in most Go environments
	path, err := e.LookPath("go")
	if err != nil {
		t.Fatalf("LookPath(\"go\") failed: %v", err)
	}
	if path == "" {
		t.Error("LookPath(\"go\") returned empty path")
	}

	// Negative case: nonexistent command
	_, err = e.LookPath("nonexistent-command-xyz-123")
	if err == nil {
		t.Error("expected error for nonexistent command, got nil")
	}
}

func TestRealExecutor_OutputMethods(t *testing.T) {
	e := &RealExecutor{}
	ctx := context.Background()

	t.Run("Output go version", func(t *testing.T) {
		out, err := e.Output(ctx, "go", "version")
		if err != nil {
			t.Fatalf("Output(go version) failed: %v", err)
		}
		if !strings.Contains(string(out), "go version") {
			t.Errorf("expected go version in output, got %q", out)
		}
	})

	t.Run("CombinedOutput go version", func(t *testing.T) {
		out, err := e.CombinedOutput(ctx, "go", "version")
		if err != nil {
			t.Fatalf("CombinedOutput(go version) failed: %v", err)
		}
		if !strings.Contains(string(out), "go version") {
			t.Errorf("expected go version in output, got %q", out)
		}
	})
}
