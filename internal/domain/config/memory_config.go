// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"strings"
)

// MemoryLearnTier is the automatic-learning tier of Memory. Exactly one tier
// is active; batch is the default.
type MemoryLearnTier string

const (
	MemoryLearnOff     MemoryLearnTier = "off"
	MemoryLearnCapture MemoryLearnTier = "capture"
	MemoryLearnBatch   MemoryLearnTier = "batch"
	MemoryLearnFull    MemoryLearnTier = "full"
)

// defaultMemoryInjectBudget is the default token budget for plur_inject_hybrid.
const defaultMemoryInjectBudget = 2000

// defaultMemoryMaxLearnsPerSession is the default flood bound for the full tier.
const defaultMemoryMaxLearnsPerSession = 3

// MemoryConfig defines configuration for automatic PLUR memory integration
// (YAML key MEMORY). Mirrors MCPServerConfig's placement: domain-owned config
// with yaml tags, validated at load and on every hot-reload re-parse.
type MemoryConfig struct {
	Enabled             bool            `yaml:"ENABLED"`
	Server              string          `yaml:"SERVER"`                 // key of the MCP_SERVERS entry backing memory; session-fixed (restart-level)
	InjectBudget        int             `yaml:"INJECT_BUDGET"`          // tokens for plur_inject_hybrid per turn; hot-reloadable
	LearnTier           MemoryLearnTier `yaml:"LEARN"`                  // off|capture|batch|full; default batch; hot-reloadable
	Scope               string          `yaml:"SCOPE"`                  // optional scope override; precedence: override-if-set -> .plur.yaml -> one surfaced warning
	MaxLearnsPerSession int             `yaml:"MAX_LEARNS_PER_SESSION"` // flood bound for the full tier; hot-reloadable
}

// DefaultMemoryConfig returns the zero-behavior default: disabled, server
// "plur", batch tier, budget 2000, flood bound 3. Enabled defaults to false
// so existing users see zero behavior change.
func DefaultMemoryConfig() MemoryConfig {
	return MemoryConfig{
		Enabled:             false,
		Server:              "plur",
		InjectBudget:        defaultMemoryInjectBudget,
		LearnTier:           MemoryLearnBatch,
		MaxLearnsPerSession: defaultMemoryMaxLearnsPerSession,
	}
}

// normalizeTier trims and lowercases the tier label.
func normalizeTier(t MemoryLearnTier) MemoryLearnTier {
	return MemoryLearnTier(strings.ToLower(strings.TrimSpace(string(t))))
}

// EffectiveLearnTier resolves the active tier, normalizing case/whitespace
// and defaulting an empty value to batch (mirrors MCPServerConfig.EffectiveAuth).
func (c *MemoryConfig) EffectiveLearnTier() MemoryLearnTier {
	if c.LearnTier == "" {
		return MemoryLearnBatch
	}
	return normalizeTier(c.LearnTier)
}

// validate reports hard configuration errors. Mirrors MCPServerConfig.validate:
// most-specific-error-wins ordering, MEMORY.-prefixed messages, empty tier
// accepted (means default batch).
func (c *MemoryConfig) validate() error {
	if c.Enabled && strings.TrimSpace(c.Server) == "" {
		return fmt.Errorf("MEMORY.SERVER must not be empty when ENABLED is true")
	}
	if c.InjectBudget < 0 {
		return fmt.Errorf("MEMORY.INJECT_BUDGET must be >= 0, got %d", c.InjectBudget)
	}
	if c.MaxLearnsPerSession < 0 {
		return fmt.Errorf("MEMORY.MAX_LEARNS_PER_SESSION must be >= 0, got %d", c.MaxLearnsPerSession)
	}
	switch c.EffectiveLearnTier() {
	case MemoryLearnOff, MemoryLearnCapture, MemoryLearnBatch, MemoryLearnFull:
		return nil
	default:
		return fmt.Errorf("MEMORY.LEARN must be one of %q, %q, %q, %q, got %q",
			MemoryLearnOff, MemoryLearnCapture, MemoryLearnBatch, MemoryLearnFull, c.EffectiveLearnTier())
	}
}
