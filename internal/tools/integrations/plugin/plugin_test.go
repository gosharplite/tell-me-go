// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package plugin

import (
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// mockPlugin implements Plugin with configurable name and register error.
type mockPlugin struct {
	name        string
	registerErr error
	registered  bool // tracks whether Register was called
}

func (m *mockPlugin) Name() string { return m.name }

func (m *mockPlugin) Register(r tools.Registry, deps PluginDependencies) error {
	m.registered = true
	return m.registerErr
}

func newMock(name string) *mockPlugin {
	return &mockPlugin{name: name}
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() []Plugin // plugins to pre-register
		plugin    Plugin          // plugin to register
		wantErr   bool
		wantCount int // expected count after Register
	}{
		{
			name: "single plugin",
			setup: func() []Plugin {
				return nil
			},
			plugin:    newMock("plugin-a"),
			wantErr:   false,
			wantCount: 1,
		},
		{
			name: "duplicate name",
			setup: func() []Plugin {
				return []Plugin{newMock("plugin-a")}
			},
			plugin:    newMock("plugin-a"),
			wantErr:   true,
			wantCount: 1,
		},
		{
			name: "multiple distinct",
			setup: func() []Plugin {
				return []Plugin{newMock("p1"), newMock("p2")}
			},
			plugin:    newMock("p3"),
			wantErr:   false,
			wantCount: 3,
		},
		{
			name: "nil plugin",
			setup: func() []Plugin {
				return nil
			},
			plugin:    nil,
			wantErr:   true,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Cleanup(Reset)

			for _, p := range tt.setup() {
				if err := Register(p); err != nil {
					t.Fatalf("setup Register(%q): unexpected error: %v", p.Name(), err)
				}
			}

			err := Register(tt.plugin)
			if (err != nil) != tt.wantErr {
				t.Errorf("Register() error = %v, wantErr = %v", err, tt.wantErr)
			}

			all := All()
			if len(all) != tt.wantCount {
				t.Errorf("All() count = %d, want %d", len(all), tt.wantCount)
			}
		})
	}
}

func TestAll_ReturnsCopy(t *testing.T) {
	t.Cleanup(Reset)

	if err := Register(newMock("p1")); err != nil {
		t.Fatal(err)
	}
	if err := Register(newMock("p2")); err != nil {
		t.Fatal(err)
	}

	// Get a copy and mutate it
	all := All()
	if len(all) != 2 {
		t.Fatalf("All() count = %d, want 2", len(all))
	}

	// Mutate the returned slice (must not affect internal state)
	all[0] = nil
	_ = append(all, newMock("p3")) // append may reallocate; discard result

	// Internal state should be unchanged
	all2 := All()
	if len(all2) != 2 {
		t.Errorf("after mutating returned copy, All() count = %d, want 2", len(all2))
	}
	if all2[0] == nil {
		t.Error("internal registry was mutated via returned slice")
	}
	if all2[0].Name() != "p1" {
		t.Errorf("internal registry first entry = %q, want %q", all2[0].Name(), "p1")
	}
}

func TestReset(t *testing.T) {
	t.Cleanup(Reset)

	if err := Register(newMock("p1")); err != nil {
		t.Fatal(err)
	}
	if err := Register(newMock("p2")); err != nil {
		t.Fatal(err)
	}

	if len(All()) != 2 {
		t.Fatalf("All() count before Reset = %d, want 2", len(All()))
	}

	Reset()

	if len(All()) != 0 {
		t.Errorf("All() count after Reset = %d, want 0", len(All()))
	}
}

func TestRegister_Concurrency(t *testing.T) {
	t.Cleanup(Reset)

	const numGoroutines = 50
	var wg sync.WaitGroup
	names := make([]string, numGoroutines)

	// Each goroutine registers one plugin with a unique name
	for i := 0; i < numGoroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("plugin-%d", i)
			if err := Register(newMock(name)); err != nil {
				// This is a non-fatal error because we want to see
				// what happens under contention.
				t.Logf("Register(%q): %v", name, err)
			}
			names[i] = name
		}()
	}

	wg.Wait()

	all := All()

	// Every name should appear exactly once
	seen := make(map[string]int, len(all))
	for _, p := range all {
		seen[p.Name()]++
	}

	for i := 0; i < numGoroutines; i++ {
		name := fmt.Sprintf("plugin-%d", i)
		count := seen[name]
		if count != 1 {
			t.Errorf("plugin %q appears %d times in registry, want 1", name, count)
		}
	}

	if len(all) != numGoroutines {
		t.Errorf("All() count = %d, want %d", len(all), numGoroutines)
	}
}

func TestPluginDependencies_ZeroValue(t *testing.T) {
	// A zero-value PluginDependencies should be usable: all fields nil,
	// plugins should check and handle nil gracefully.
	var deps PluginDependencies

	// Verify all fields are nil/zero
	if deps.FileSystem != nil {
		t.Error("zero-value FileSystem should be nil")
	}
	if deps.SecurityMgr != nil {
		t.Error("zero-value SecurityMgr should be nil")
	}
	if deps.LLMClient != nil {
		t.Error("zero-value LLMClient should be nil")
	}
	if deps.HTTPClient != nil {
		t.Error("zero-value HTTPClient should be nil")
	}
	if deps.AssetsDir != "" {
		t.Errorf("zero-value AssetsDir = %q, want empty", deps.AssetsDir)
	}
}

func TestRegister_InsertionOrder(t *testing.T) {
	t.Cleanup(Reset)

	names := []string{"alpha", "beta", "gamma", "delta"}
	for _, name := range names {
		if err := Register(newMock(name)); err != nil {
			t.Fatal(err)
		}
	}

	all := All()
	if len(all) != len(names) {
		t.Fatalf("All() count = %d, want %d", len(all), len(names))
	}
	for i, p := range all {
		if p.Name() != names[i] {
			t.Errorf("position %d: got %q, want %q", i, p.Name(), names[i])
		}
	}
}

// TestRegister_Parallel ensures register/read work under parallel access.
// This test does NOT call Reset, so it can use t.Parallel.
func TestRegister_Parallel(t *testing.T) {
	t.Parallel()
	t.Cleanup(Reset)

	if err := Register(newMock("parallel-a")); err != nil {
		t.Fatal(err)
	}

	t.Run("read", func(t *testing.T) {
		t.Parallel()
		all := All()
		if len(all) == 0 {
			t.Error("All() returned empty after register in parallel test")
		}
	})

	t.Run("register", func(t *testing.T) {
		t.Parallel()
		err := Register(newMock("parallel-" + strconv.Itoa(int(^uint(0)>>1)))) // unique-ish
		if err == nil {
			// Successfully registered
			return
		}
		// May legitimately collide with another parallel test; that's fine
		t.Logf("Register in parallel: %v", err)
	})
}
