// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/exec"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"

	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestRegister(t *testing.T) {
	reg := registry.New()
	sm := security.NewSecurityManager(nil)
	if err := Register(reg, sm, &exec.RealExecutor{}, security.NewCommandValidator(sm, nil), infrapersistence.NewOSFileSystem()); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	decls := reg.GetDeclarations()
	found := make(map[string]bool)
	for _, d := range decls {
		found[d.Name] = true
	}

	expectedTools := []string{
		"list_files",
		"get_tree",
		"read_file",
		"read_files",
		"search_files",
		"replace_text",
		"find_file",
		"get_definitions",
		"write_file",
		"append_text",
		"get_file_diff",
		"undo_file_change",
		"execute_command",
		"pipe_commands",
		"ask_user",
		"get_git_status",
		"get_git_diff",
		"get_git_log",
		"get_git_show",
		"get_git_blame",
		"git_commit",
		"git_create_branch",
	}

	for _, name := range expectedTools {
		if !found[name] {
			t.Errorf("tool %s not registered", name)
		}
	}
}
