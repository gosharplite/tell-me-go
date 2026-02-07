// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package registry_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestRegistry_Resilience(t *testing.T) {
	t.Run("panic on empty tool name", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("The code did not panic")
			} else {
				errMsg := r.(string)
				if errMsg != "cannot register tool with empty name" {
					t.Errorf("Unexpected panic message: %s", errMsg)
				}
			}
		}()

		r := registry.New()
		r.Register(&tools.ToolDeclaration{Name: ""}, nil)
	})

	t.Run("error on unknown tool execution", func(t *testing.T) {
		r := registry.New()
		_, err := r.Execute(context.Background(), "unknown", nil)
		if err == nil {
			t.Fatal("expected error for unknown tool, got nil")
		}
		if !strings.Contains(err.Error(), "tool not found") {
			t.Errorf("expected 'tool not found' error, got: %v", err)
		}
	})

	t.Run("wrapped error on handler failure", func(t *testing.T) {
		r := registry.New()
		targetErr := errors.New("something went wrong")
		r.Register(&tools.ToolDeclaration{Name: "failer"}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
			return tools.ToolResult{}, targetErr
		})

		_, err := r.Execute(context.Background(), "failer", nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, targetErr) {
			t.Errorf("expected wrapped error to contain targetErr, got: %v", err)
		}
		if !strings.Contains(err.Error(), "tool execution failed: failer") {
			t.Errorf("expected error message to contain tool name and failure context, got: %v", err)
		}
	})
}
