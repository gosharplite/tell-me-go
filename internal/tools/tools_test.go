// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools_test

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestNewToolRegistry(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	r := registry.New()
	tools.RegisterAll(r, sm, t.TempDir(), "v1.0.0", nil)

	if len(r.GetDeclarations()) == 0 {
		t.Error("expected registered tools, got none")
	}
}

func TestToolExecution(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	r := registry.New()
	tools.RegisterAll(r, sm, t.TempDir(), "v1.0.0", nil)

	ctx := context.Background()
	// list_files is registered by files.Register
	_, err := r.Execute(ctx, "list_files", map[string]interface{}{"path": "."})
	if err != nil {
		t.Errorf("failed to execute list_files: %v", err)
	}
}
