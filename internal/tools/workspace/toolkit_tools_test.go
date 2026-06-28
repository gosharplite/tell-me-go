// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type mockToolkitSessionProvider struct {
	ports.SessionProvider
	info ports.SessionInfo
}

func (m *mockToolkitSessionProvider) GetInfo() ports.SessionInfo {
	return m.info
}

func (m *mockToolkitSessionProvider) SetInfo(_ context.Context, info ports.SessionInfo) error {
	m.info = info
	return nil
}

func (m *mockToolkitSessionProvider) GetTasks() ports.TaskStore {
	return nil
}

func TestHandleLoadToolkit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		requestedToolkits []string
		availableToolkits []string
		initialToolkits   []string
		expectedActive    []string
		expectedInOutput  []string
	}{
		{
			name:              "load single valid toolkit",
			requestedToolkits: []string{"git"},
			availableToolkits: []string{"core", "git", "k8s"},
			initialToolkits:   []string{},
			expectedActive:    []string{"git"},
			expectedInOutput:  []string{"Successfully loaded toolkits: [git]"},
		},
		{
			name:              "load multiple valid toolkits",
			requestedToolkits: []string{"git", "k8s"},
			availableToolkits: []string{"core", "git", "k8s", "ado"},
			initialToolkits:   []string{},
			expectedActive:    []string{"git", "k8s"},
			expectedInOutput:  []string{"Successfully loaded toolkits: [git, k8s]"},
		},
		{
			name:              "ignore unknown toolkits",
			requestedToolkits: []string{"git", "unknown"},
			availableToolkits: []string{"core", "git"},
			initialToolkits:   []string{},
			expectedActive:    []string{"git"},
			expectedInOutput:  []string{"Successfully loaded toolkits: [git]", "Warning: Unknown toolkits requested and skipped: [unknown]"},
		},
		{
			name:              "do not duplicate active toolkits",
			requestedToolkits: []string{"git"},
			availableToolkits: []string{"core", "git"},
			initialToolkits:   []string{"git"},
			expectedActive:    []string{"git"},
			expectedInOutput:  []string{"Toolkits already active: [git]"},
		},
		{
			name:              "nothing to load",
			requestedToolkits: []string{},
			availableToolkits: []string{"core", "git"},
			initialToolkits:   []string{},
			expectedActive:    []string{},
			expectedInOutput:  []string{"No toolkits were loaded"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sp := &mockToolkitSessionProvider{
				info: ports.SessionInfo{
					ActiveToolkits: tt.initialToolkits,
				},
			}
			mp := &mockMetadataProvider{
				toolkits: tt.availableToolkits,
			}

			pt := newpersistenceTools(sp, mp)

			args := map[string]interface{}{
				"names": tt.requestedToolkits,
			}

			res, err := pt.handleLoadToolkit(context.Background(), args, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify session info
			active := sp.GetInfo().ActiveToolkits
			if len(active) != len(tt.expectedActive) {
				t.Errorf("expected %d active toolkits, got %d", len(tt.expectedActive), len(active))
			}

			activeMap := make(map[string]bool)
			for _, tk := range active {
				activeMap[tk] = true
			}
			for _, tk := range tt.expectedActive {
				if !activeMap[tk] {
					t.Errorf("expected toolkit %s to be active", tk)
				}
			}

			// Verify output
			for _, expected := range tt.expectedInOutput {
				if !strings.Contains(res.Text, expected) {
					t.Errorf("expected output to contain %q, got %q", expected, res.Text)
				}
			}
		})
	}
}

// TestHandleLoadToolkit_UnmarshalError verifies that passing malformed args
// (where "names" is not []string) triggers an UnmarshalArgs failure, and the
// returned ToolResult is zero-valued.
func TestHandleLoadToolkit_UnmarshalError(t *testing.T) {
	t.Parallel()

	sp := &mockToolkitSessionProvider{
		info: ports.SessionInfo{},
	}
	mp := &mockMetadataProvider{
		toolkits: []string{"core", "git"},
	}

	pt := newpersistenceTools(sp, mp)

	// "names" should be []string but we pass an int
	args := map[string]interface{}{
		"names": 42,
	}

	res, err := pt.handleLoadToolkit(context.Background(), args, nil)
	if err == nil {
		t.Fatal("expected error for malformed args, got nil")
	}

	// ToolResult should be zero-valued on error
	if res.Text != "" {
		t.Errorf("expected empty Text on error, got %q", res.Text)
	}
	if res.Error != nil {
		t.Errorf("expected nil Error field, got %v", res.Error)
	}
	if len(res.BinaryData) != 0 {
		t.Errorf("expected empty BinaryData, got %v", res.BinaryData)
	}
	if len(res.Metadata) != 0 {
		t.Errorf("expected empty Metadata, got %v", res.Metadata)
	}
}
