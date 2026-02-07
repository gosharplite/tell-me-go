// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestRegister(t *testing.T) {
	reg := registry.New()
	sm := security.NewSecurityManager(nil)
	Register(reg, sm)

	decls := reg.GetDeclarations()
	found := make(map[string]bool)
	for _, d := range decls {
		found[d.Name] = true
	}

	expectedTools := []string{
		"list_files",
		"get_tree",
		"read_file",
		"search_files",
		"replace_text",
		"find_file",
		"get_definitions",
		"write_file",
		"append_text",
		"get_file_diff",
		"undo_file_change",
	}

	for _, name := range expectedTools {
		if !found[name] {
			t.Errorf("tool %s not registered", name)
		}
	}
}
