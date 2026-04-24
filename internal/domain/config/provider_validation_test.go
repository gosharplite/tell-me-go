// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// This file pins the contract for the LLMProvider.Validate() method
// and Config.ValidateProviders() helper introduced by Task H. The
// validation lives in the domain layer (not infrastructure) because
// "MaxTokens must be >= 0" is a semantic invariant of LLMProvider, not
// an I/O concern of YAML parsing.

// newWarnBuffer returns a slog.Logger that writes warn-level (and
// above) records to the returned buffer. Used to assert on warning
// emissions without coupling the test to the global default logger.
func newWarnBuffer() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	handler := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	return slog.New(handler), buf
}

// TestLLMProvider_Validate_NegativeMaxTokensRejected pins the headline
// hard-reject contract: any negative MAX_TOKENS value fails validation
// with an actionable error message that names the provider and the
// offending value.
//
// FAILURE MEANING: If validation accepts negative values, operators
// can ship YAML that the API will later reject with a generic 400 —
// the loader's job is to catch this at startup with a clear message.
func TestLLMProvider_Validate_NegativeMaxTokensRejected(t *testing.T) {
	t.Parallel()
	logger, _ := newWarnBuffer()
	p := LLMProvider{Type: "anthropic", MaxTokens: -1}
	err := p.validate("claude", logger)
	if err == nil {
		t.Fatal("expected error for negative MAX_TOKENS, got nil")
	}
	if !strings.Contains(err.Error(), "MAX_TOKENS") {
		t.Errorf("error must mention MAX_TOKENS for diagnosability; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error must name the offending provider; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "-1") {
		t.Errorf("error must include the offending value; got %q", err.Error())
	}
}

// TestLLMProvider_Validate_ZeroAccepted asserts that the unset case
// (MaxTokens == 0) is the normal happy path — the provider's package
// default applies at request time.
func TestLLMProvider_Validate_ZeroAccepted(t *testing.T) {
	t.Parallel()
	logger, _ := newWarnBuffer()
	p := LLMProvider{Type: "anthropic", MaxTokens: 0}
	if err := p.validate("claude", logger); err != nil {
		t.Errorf("MaxTokens=0 (unset) must validate; got %v", err)
	}
}

// TestLLMProvider_Validate_PositiveAccepted asserts that any positive
// value passes validation. Soft-warning for absurdly large values is
// the factory's responsibility, not the domain's (the domain doesn't
// know which model a value is for).
func TestLLMProvider_Validate_PositiveAccepted(t *testing.T) {
	t.Parallel()
	logger, _ := newWarnBuffer()
	cases := []int{1, 4096, 16384, 65000, 999999999}
	for _, n := range cases {
		p := LLMProvider{Type: "anthropic", MaxTokens: n}
		if err := p.validate("claude", logger); err != nil {
			t.Errorf("positive MaxTokens=%d must validate; got %v", n, err)
		}
	}
}

// TestLLMProvider_Validate_AnthropicBelowThinkingBudgetFloorWarns
// pins the architect's Decision 8 follow-up: when an Anthropic
// provider has MaxTokens > 0 AND ThinkingBudget > 0 AND
// MaxTokens < ThinkingBudget + 1024, the runtime will silently bump
// max_tokens at request time. We surface this silent bump as a
// warning so operators can see it without tracing through the
// Anthropic client.
//
// FAILURE MEANING: Operators with this misconfiguration will be
// surprised by the runtime bump — the warning is the only visible
// signal that the configured cap is being overridden.
func TestLLMProvider_Validate_AnthropicBelowThinkingBudgetFloorWarns(t *testing.T) {
	t.Parallel()
	logger, buf := newWarnBuffer()
	p := LLMProvider{
		Type:           "anthropic",
		MaxTokens:      4096,  // below the 32768 + 1024 floor
		ThinkingBudget: 32768, // typical "high reasoning" budget
	}
	if err := p.validate("claude", logger); err != nil {
		t.Fatalf("validation must not return error for warn-only case; got %v", err)
	}
	logged := buf.String()
	if !strings.Contains(logged, "provider_max_tokens_below_thinking_budget_floor") {
		t.Errorf("expected warning key 'provider_max_tokens_below_thinking_budget_floor' in log; got %q", logged)
	}
	if !strings.Contains(logged, "claude") {
		t.Errorf("warning must name the provider; got %q", logged)
	}
}

// TestLLMProvider_Validate_NonAnthropicBelowThinkingBudgetFloorDoesNotWarn
// is the negative control: the Anthropic-specific runtime-bump
// behavior does not exist for Gemini or OpenAI, so the warning must
// not fire for them.
//
// FAILURE MEANING: A spurious warning misleads operators into thinking
// their Gemini config has a problem when it doesn't.
func TestLLMProvider_Validate_NonAnthropicBelowThinkingBudgetFloorDoesNotWarn(t *testing.T) {
	t.Parallel()
	for _, providerType := range []string{"gemini", "google", "openai", "deepseek"} {
		t.Run(providerType, func(t *testing.T) {
			t.Parallel()
			logger, buf := newWarnBuffer()
			p := LLMProvider{
				Type:           providerType,
				MaxTokens:      4096,
				ThinkingBudget: 32768,
			}
			if err := p.validate("test", logger); err != nil {
				t.Fatalf("validation must not return error; got %v", err)
			}
			if strings.Contains(buf.String(), "provider_max_tokens_below_thinking_budget_floor") {
				t.Errorf("anthropic-specific warning must not fire for %s; got %q", providerType, buf.String())
			}
		})
	}
}

// TestConfig_ValidateProviders_PropagatesError pins that
// Config.ValidateProviders short-circuits on the first invalid
// provider in the map and returns its error verbatim.
func TestConfig_ValidateProviders_PropagatesError(t *testing.T) {
	t.Parallel()
	logger, _ := newWarnBuffer()
	cfg := &Config{
		Providers: map[string]LLMProvider{
			"good": {Type: "anthropic", MaxTokens: 8192},
			"bad":  {Type: "anthropic", MaxTokens: -42},
		},
	}
	err := cfg.ValidateProviders(logger)
	if err == nil {
		t.Fatal("expected error from bad provider, got nil")
	}
	assert.Contains(t, err.Error(), "bad")
	assert.Contains(t, err.Error(), "MAX_TOKENS")
	assert.Contains(t, err.Error(), "-42")
}

// TestConfig_ValidateProviders_AllValidReturnsNil is the happy-path
// negative control: a config with only valid providers must validate
// cleanly without error.
func TestConfig_ValidateProviders_AllValidReturnsNil(t *testing.T) {
	t.Parallel()
	logger, _ := newWarnBuffer()
	cfg := &Config{
		Providers: map[string]LLMProvider{
			"google":    {Type: "gemini", MaxTokens: 8192},
			"claude":    {Type: "anthropic", MaxTokens: 16384},
			"openai":    {Type: "openai", MaxTokens: 0}, // unset is OK
			"deepseek":  {Type: "deepseek"},             // omitted is OK
			"barebones": {Type: "openai", MaxTokens: 32000},
		},
	}
	if err := cfg.ValidateProviders(logger); err != nil {
		t.Errorf("all-valid config must validate cleanly; got %v", err)
	}
}
