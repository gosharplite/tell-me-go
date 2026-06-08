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

	tests := []struct {
		name    string
		path    string
		setup   func(m *MockConfigLoader)
		wantNil bool
		wantErr bool
	}{
		{
			name: "success path: non-nil args.Get(0) returns config and nil error",
			path: "/test/config.yaml",
			setup: func(m *MockConfigLoader) {
				m.On("Load", "/test/config.yaml").Return(&config.Config{}, nil)
			},
			wantNil: false,
			wantErr: false,
		},
		{
			name: "error path with typed nil: args.Get(0) != nil, returns typed nil config and error",
			path: "/test/typed-nil.yaml",
			setup: func(m *MockConfigLoader) {
				m.On("Load", "/test/typed-nil.yaml").Return((*config.Config)(nil), errors.New("load failed"))
			},
			wantNil: true,
			wantErr: true,
		},
		{
			name: "defensive nil path: args.Get(0) == nil returns hardcoded nil config and error",
			path: "/test/defensive.yaml",
			setup: func(m *MockConfigLoader) {
				// Passing nil through an explicit interface{} variable
				// preserves the untyped nil so args.Get(0) == nil is true,
				// triggering the defensive nil-guard branch.
				var nilCfg interface{}
				m.On("Load", "/test/defensive.yaml").Return(nilCfg, errors.New("defensive load failed"))
			},
			wantNil: true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := new(MockConfigLoader)
			tt.setup(m)

			cfg, err := m.Load(tt.path)

			if tt.wantNil && cfg != nil {
				t.Errorf("expected nil config, got %+v", cfg)
			}
			if !tt.wantNil && cfg == nil {
				t.Error("expected non-nil config, got nil")
			}
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			m.AssertExpectations(t)
		})
	}
}
