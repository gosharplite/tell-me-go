// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"strings"
	"testing"

	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

func TestRuntimeConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		cfg       RuntimeConfig
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid minimal config",
			cfg: RuntimeConfig{
				ProviderName: "openai",
				Model:        "gpt-4",
				Mode:         "chat",
			},
		},
		{
			name: "valid with non-empty pricing override",
			cfg: RuntimeConfig{
				ProviderName:     "openai",
				Model:            "gpt-4",
				Mode:             "chat",
				PricingOverrides: map[string]domain_pricing.ModelPricing{"gpt-4": {}},
			},
		},
		{
			name: "valid with nil pricing overrides",
			cfg: RuntimeConfig{
				ProviderName:     "openai",
				Model:            "gpt-4",
				Mode:             "chat",
				PricingOverrides: nil,
			},
		},
		{
			name:      "empty provider name",
			cfg:       RuntimeConfig{Model: "gpt-4", Mode: "chat"},
			wantErr:   true,
			errSubstr: "provider name",
		},
		{
			name:      "empty model",
			cfg:       RuntimeConfig{ProviderName: "openai", Mode: "chat"},
			wantErr:   true,
			errSubstr: "model",
		},
		{
			name:      "empty mode",
			cfg:       RuntimeConfig{ProviderName: "openai", Model: "gpt-4"},
			wantErr:   true,
			errSubstr: "mode",
		},
		{
			name: "empty pricing override key",
			cfg: RuntimeConfig{
				ProviderName:     "openai",
				Model:            "gpt-4",
				Mode:             "chat",
				PricingOverrides: map[string]domain_pricing.ModelPricing{"": {}},
			},
			wantErr:   true,
			errSubstr: "empty key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errSubstr)
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error = %q; want substring %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
