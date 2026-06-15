// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
)

func FuzzLoadConfig(f *testing.F) {
	// ── Seed corpus ────────────────────────────────────────────────

	// Seed 1 — Minimal valid config
	f.Add([]byte("MODE: test-mode\nPERSON: test-person\nAIMODEL: test-model\nAIURL: http://test.url"))

	// Seed 2 — Provider with env expansion
	f.Add([]byte("SELECTED_PROVIDER: work-openai\nPROVIDERS:\n  work-openai:\n    TYPE: openai\n    API_KEY: ${MOCK_SECRET}\n    MODEL: gpt-4"))

	// Seed 3 — Model with dots/slashes
	f.Add([]byte("MODELS:\n  \"deepseek-ai/deepseek-v3.2-maas\":\n    CONTEXT_WINDOW: 163840\n    PRICING:\n      COMP: 5.40"))

	// Seed 4 — Provider max_tokens
	f.Add([]byte("SELECTED_PROVIDER: claude\nPROVIDERS:\n  claude:\n    TYPE: anthropic\n    MODEL: claude-opus-4-6\n    API_KEY: test\n    MAX_TOKENS: 16384"))

	// Seed 5 — Invalid YAML from existing test
	f.Add([]byte(": invalid"))

	// Seed 6 — Type mismatch (scalar where map expected)
	f.Add([]byte("MODE: test\nMODELS: this-is-a-string-not-a-map"))

	// Seed 7 — Empty input
	f.Add([]byte(""))

	// Seed 8 — Binary garbage
	f.Add([]byte("\x00\xFF\xFE"))

	// Seed 9 — Long key (200 "a" chars)
	f.Add([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa: 1"))

	// Seed 10 — Duplicate keys
	f.Add([]byte("MODE: first\nMODE: second"))

	// Seed 11 — Deeply nested maps (50 levels)
	func() {
		var sb strings.Builder
		for i := 0; i < 50; i++ {
			sb.WriteString(strings.Repeat(" ", i))
			sb.WriteString("a:\n")
		}
		sb.WriteString(strings.Repeat(" ", 50))
		sb.WriteString("leaf_value")
		sb.WriteString(strings.Repeat("\n", 50))
		f.Add([]byte(sb.String()))
	}()

	// Seed 12 — Control character in key
	f.Add([]byte("MOD\x01E: value"))

	// Seed 13 — Null byte embedded in string value
	f.Add([]byte("MODE: \"test\x00injected\""))

	// Seed 14 — Unicode/emoji in keys and values
	f.Add([]byte("MODE: \"🚀 production 🚀\"\nPERSON: \"José 🌍\""))

	// Seed 15 — Max_tokens negative (should fail validation)
	f.Add([]byte("SELECTED_PROVIDER: bad\nPROVIDERS:\n  bad:\n    TYPE: openai\n    API_KEY: x\n    MAX_TOKENS: -1"))

	// ── Fuzz function ──────────────────────────────────────────────

	// ensure domain_config import is retained for type resolution
	_ = (*domain_config.Config)(nil)

	f.Fuzz(func(t *testing.T, data []byte) {
		// Neutralize ambient environment
		t.Setenv("TELL_ME_MODE", "")
		t.Setenv("GOSHARP_MODE", "")
		t.Setenv("GOSHARP_PERSON", "")
		t.Setenv("GOSHARP_AIMODEL", "")
		t.Setenv("GOSHARP_AIURL", "")

		// Write fuzzed data to a temp file
		configPath := filepath.Join(t.TempDir(), "fuzz_config.yaml")
		if err := os.WriteFile(configPath, data, 0644); err != nil {
			t.Skipf("failed to write fuzz config: %v", err)
			return
		}

		// Catch panics escaping load()
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("load() panicked with: %v", r)
			}
		}()

		cfg, err := load(configPath)
		if err != nil {
			// Expected: malformed input should error
			return
		}

		if cfg == nil {
			t.Error("load() returned nil config with nil error")
			return
		}

		// ── Post-load invariants ────────────────────────────────

		if cfg.MaxToolTurns < 0 {
			t.Errorf("MaxToolTurns is negative: %d", cfg.MaxToolTurns)
		}

		for name, p := range cfg.Providers {
			if p.MaxTokens < 0 {
				t.Errorf("provider %q has negative MaxTokens: %d", name, p.MaxTokens)
			}
		}
	})
}
