// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/stretchr/testify/assert"
)

type mockToolkitSessionProvider struct {
	info ports.SessionInfo
}

func (m *mockToolkitSessionProvider) GetInfo() ports.SessionInfo {
	return m.info
}

func (m *mockToolkitSessionProvider) SetInfo(info ports.SessionInfo) {
	m.info = info
}

func (m *mockToolkitSessionProvider) GetTasks() ports.TaskStore      { return nil }
func (m *mockToolkitSessionProvider) GetSettings() ports.KVStore     { return nil }
func (m *mockToolkitSessionProvider) Close() error                   { return nil }

func TestDynamicToolkitDiscovery(t *testing.T) {
	// This test bypasses the full Agent and tests the ContextManager/Registry interaction
	// that inferenceStep uses.
	
	reg := registry.New()
	sp := &mockToolkitSessionProvider{
		info: ports.SessionInfo{
			ActiveToolkits: []string{}, // Initially empty
		},
	}
	
	// Register some tools
	_ = reg.RegisterToToolkit("core", &tools.ToolDeclaration{Name: "core_tool"}, nil)
	_ = reg.RegisterToToolkit("git", &tools.ToolDeclaration{Name: "git_tool"}, nil)
	
	t.Run("Initially only core tools are visible", func(t *testing.T) {
		activeToolkits := sp.GetInfo().ActiveToolkits
		activeTools := reg.GetDeclarationsByToolkits(activeToolkits)
		
		assert.Len(t, activeTools, 1)
		assert.Equal(t, "core_tool", activeTools[0].Name)
	})
	
	t.Run("After loading git toolkit, git tools become visible", func(t *testing.T) {
		// Simulate load_toolkit execution
		info := sp.GetInfo()
		info.ActiveToolkits = append(info.ActiveToolkits, "git")
		sp.SetInfo(info)
		
		activeToolkits := sp.GetInfo().ActiveToolkits
		activeTools := reg.GetDeclarationsByToolkits(activeToolkits)
		
		assert.Len(t, activeTools, 2)
		names := []string{activeTools[0].Name, activeTools[1].Name}
		assert.Contains(t, names, "core_tool")
		assert.Contains(t, names, "git_tool")
	})
}
