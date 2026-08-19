// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"
)

// TestDefaultMemoryConfig pins the zero-behavior default: disabled, server
// "plur", batch tier, budget 2000, flood bound 3. Enabled defaults to false
// so existing users see zero behavior change.
func TestDefaultMemoryConfig(t *testing.T) {
	got := DefaultMemoryConfig()

	if got.Enabled {
		t.Error("DefaultMemoryConfig().Enabled = true; want false (zero behavior change)")
	}
	if got.Server != "plur" {
		t.Errorf("DefaultMemoryConfig().Server = %q; want %q", got.Server, "plur")
	}
	if got.InjectBudget != defaultMemoryInjectBudget {
		t.Errorf("DefaultMemoryConfig().InjectBudget = %d; want %d", got.InjectBudget, defaultMemoryInjectBudget)
	}
	if got.LearnTier != MemoryLearnBatch {
		t.Errorf("DefaultMemoryConfig().LearnTier = %q; want %q", got.LearnTier, MemoryLearnBatch)
	}
	if got.MaxLearnsPerSession != defaultMemoryMaxLearnsPerSession {
		t.Errorf("DefaultMemoryConfig().MaxLearnsPerSession = %d; want %d", got.MaxLearnsPerSession, defaultMemoryMaxLearnsPerSession)
	}
}

// TestMemoryConfig_EffectiveLearnTier pins tier resolution: an empty value
// defaults to batch; otherwise the label is normalized (lowercased and
// whitespace-trimmed) without mutating the underlying config.
func TestMemoryConfig_EffectiveLearnTier(t *testing.T) {
	tests := []struct {
		name string
		tier MemoryLearnTier
		want MemoryLearnTier
	}{
		{"empty defaults to batch", "", MemoryLearnBatch},
		{"off passthrough", MemoryLearnOff, MemoryLearnOff},
		{"capture passthrough", MemoryLearnCapture, MemoryLearnCapture},
		{"batch passthrough", MemoryLearnBatch, MemoryLearnBatch},
		{"full passthrough", MemoryLearnFull, MemoryLearnFull},
		{"uppercase normalized", "BATCH", MemoryLearnBatch},
		{"mixed case normalized", "Full", MemoryLearnFull},
		{"surrounding whitespace trimmed", " full ", MemoryLearnFull},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &MemoryConfig{LearnTier: tt.tier}
			if got := c.EffectiveLearnTier(); got != tt.want {
				t.Errorf("EffectiveLearnTier(%q) = %q; want %q", tt.tier, got, tt.want)
			}
		})
	}
}

// TestMemoryConfig_Validate pins the hard-validation contract: the default
// config and an explicit full config are valid; ENABLED with an empty SERVER,
// a negative INJECT_BUDGET, a negative MAX_LEARNS_PER_SESSION, and an unknown
// LEARN tier are all rejected with MEMORY.-prefixed messages. An empty tier
// is valid (it means default batch).
func TestMemoryConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		cfg         MemoryConfig
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid default",
			cfg:     DefaultMemoryConfig(),
			wantErr: false,
		},
		{
			name: "valid explicit full",
			cfg: MemoryConfig{
				Enabled:             true,
				Server:              "plur",
				InjectBudget:        5000,
				LearnTier:           MemoryLearnFull,
				Scope:               "team-x",
				MaxLearnsPerSession: 5,
			},
			wantErr: false,
		},
		{
			name: "enabled with empty server rejected",
			cfg: MemoryConfig{
				Enabled: true,
				Server:  "  ",
			},
			wantErr:     true,
			errContains: "MEMORY.SERVER must not be empty when ENABLED is true",
		},
		{
			name: "disabled with empty server is valid",
			cfg: MemoryConfig{
				Enabled: false,
				Server:  "",
			},
			wantErr: false,
		},
		{
			name: "negative inject budget rejected",
			cfg: MemoryConfig{
				InjectBudget: -1,
			},
			wantErr:     true,
			errContains: "MEMORY.INJECT_BUDGET must be >= 0, got -1",
		},
		{
			name: "negative max learns rejected",
			cfg: MemoryConfig{
				MaxLearnsPerSession: -1,
			},
			wantErr:     true,
			errContains: "MEMORY.MAX_LEARNS_PER_SESSION must be >= 0, got -1",
		},
		{
			name: "invalid tier rejected",
			cfg: MemoryConfig{
				LearnTier: "weekly",
			},
			wantErr:     true,
			errContains: `MEMORY.LEARN must be one of "off", "capture", "batch", "full", got "weekly"`,
		},
		{
			name: "empty tier valid (defaults to batch)",
			cfg: MemoryConfig{
				LearnTier: "",
			},
			wantErr: false,
		},
		{
			name: "mixed-case tier valid after normalization",
			cfg: MemoryConfig{
				LearnTier: " BATCH ",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("validate() expected error containing %q, got nil", tt.errContains)
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validate() error = %q; want substring %q", err.Error(), tt.errContains)
				}
			} else if err != nil {
				t.Errorf("validate() unexpected error: %v", err)
			}
		})
	}
}

// TestConfig_ValidateMemory pins the Config-level hook: it delegates to the
// embedded MemoryConfig.validate, so an invalid MEMORY section surfaces
// through Config.ValidateMemory.
func TestConfig_ValidateMemory(t *testing.T) {
	cfg := &Config{Memory: MemoryConfig{Enabled: true, Server: ""}}
	err := cfg.ValidateMemory()
	if err == nil {
		t.Fatal("ValidateMemory() expected error for ENABLED with empty SERVER, got nil")
	}
	if !strings.Contains(err.Error(), "MEMORY.SERVER") {
		t.Errorf("ValidateMemory() error = %q; want substring %q", err.Error(), "MEMORY.SERVER")
	}

	cfg = &Config{Memory: DefaultMemoryConfig()}
	if err := cfg.ValidateMemory(); err != nil {
		t.Errorf("ValidateMemory() unexpected error for default config: %v", err)
	}
}
