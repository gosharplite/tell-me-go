// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package configtest

import (
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
)

func TestMockConfigLoader_Load(t *testing.T) {
	t.Parallel()

	t.Run("success path", func(t *testing.T) {
		t.Parallel()
		var called bool
		m := &MockConfigLoader{
			LoadFunc: func(path string) (*config.Config, error) {
				called = true
				return &config.Config{}, nil
			},
		}
		cfg, err := m.Load("/test/config.yaml")
		if cfg == nil {
			t.Error("expected non-nil config")
		}
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !called {
			t.Error("expected LoadFunc to be called")
		}
	})

	t.Run("error path", func(t *testing.T) {
		t.Parallel()
		wantErr := errors.New("load failed")
		m := &MockConfigLoader{
			LoadFunc: func(path string) (*config.Config, error) {
				return nil, wantErr
			},
		}
		cfg, err := m.Load("/test/error.yaml")
		if cfg != nil {
			t.Error("expected nil config on error")
		}
		if err != wantErr {
			t.Errorf("got %v, want %v", err, wantErr)
		}
	})

	t.Run("defensive nil path (LoadFunc unset)", func(t *testing.T) {
		t.Parallel()
		m := &MockConfigLoader{} // zero value — LoadFunc is nil
		cfg, err := m.Load("/test/defensive.yaml")
		if cfg != nil {
			t.Error("expected nil config when LoadFunc is nil")
		}
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("defensive nil path (LoadFunc set, returns nil,nil)", func(t *testing.T) {
		t.Parallel()
		var called bool
		m := &MockConfigLoader{
			LoadFunc: func(path string) (*config.Config, error) {
				called = true
				return nil, nil
			},
		}
		cfg, err := m.Load("/test/defensive-set.yaml")
		if cfg != nil {
			t.Error("expected nil config when LoadFunc returns nil")
		}
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if !called {
			t.Error("expected LoadFunc to be called")
		}
	})
}
