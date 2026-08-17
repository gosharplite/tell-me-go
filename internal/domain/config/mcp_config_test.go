// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestConfig_ValidateMCPServers_KeyValidation pins the MCP server key
// format contract: keys must match ^[a-z0-9-]+$ (lowercase alphanumeric
// and hyphens). Uppercase, underscores, dots, and the empty string are
// all rejected.
func TestConfig_ValidateMCPServers_KeyValidation(t *testing.T) {
	t.Parallel()

	valid := []string{"github", "github-copilot", "server-1"}
	for _, key := range valid {
		key := key
		t.Run("valid_"+key, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{MCPServers: map[string]MCPServerConfig{
				key: {URL: "https://example.com/mcp"},
			}}
			require.NoError(t, cfg.ValidateMCPServers())
		})
	}

	invalid := []string{"GitHub", "my_server", "server.name", ""}
	for _, key := range invalid {
		key := key
		t.Run("invalid_"+key, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{MCPServers: map[string]MCPServerConfig{
				key: {URL: "https://example.com/mcp"},
			}}
			err := cfg.ValidateMCPServers()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "MCP_SERVERS")
		})
	}
}

// TestMCPServerConfig_ConsentAndSerial pins the URL-class defaults for
// consent and serialization, plus the explicit-override precedence:
//
//   - "/readonly" endpoints: read-only, no consent, concurrent.
//   - mutating endpoints: not read-only, consent required, serial.
//   - explicit REQUIRES_CONSENT always wins over the URL-class default.
func TestMCPServerConfig_ConsentAndSerial(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		name         string
		cfg          MCPServerConfig
		wantReadOnly bool
		wantConsent  bool
		wantSerial   bool
	}{
		{
			name:         "readonly endpoint defaults",
			cfg:          MCPServerConfig{URL: "https://api.githubcopilot.com/mcp/readonly"},
			wantReadOnly: true,
			wantConsent:  false,
			wantSerial:   false,
		},
		{
			name:         "mutating endpoint defaults",
			cfg:          MCPServerConfig{URL: "https://api.githubcopilot.com/mcp/"},
			wantReadOnly: false,
			wantConsent:  true,
			wantSerial:   true,
		},
		{
			name:         "explicit consent true overrides readonly default",
			cfg:          MCPServerConfig{URL: "https://api.githubcopilot.com/mcp/readonly", RequiresConsent: &trueVal},
			wantReadOnly: true,
			wantConsent:  true,
			wantSerial:   false,
		},
		{
			name:         "explicit consent false overrides mutating default",
			cfg:          MCPServerConfig{URL: "https://api.githubcopilot.com/mcp/", RequiresConsent: &falseVal},
			wantReadOnly: false,
			wantConsent:  false,
			wantSerial:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantReadOnly, tt.cfg.IsReadOnly())
			assert.Equal(t, tt.wantConsent, tt.cfg.EffectiveRequiresConsent())
			assert.Equal(t, tt.wantSerial, tt.cfg.EffectiveSerial())
		})
	}
}

// TestMCPServerConfig_EffectiveTimeout pins timeout defaulting: zero falls
// back to DefaultMCPTimeoutSeconds (300s); a positive value is honored.
func TestMCPServerConfig_EffectiveTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		timeout int
		want    time.Duration
	}{
		{"default when zero", 0, 300 * time.Second},
		{"explicit positive", 60, 60 * time.Second},
		{"explicit large", 3600, 3600 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := MCPServerConfig{Timeout: tt.timeout}
			assert.Equal(t, tt.want, c.EffectiveTimeout())
		})
	}
}

// TestMCPServerConfig_Validate pins the per-server hard-validation
// contract: URL must be non-empty (after trim) and Timeout must be >= 0.
func TestMCPServerConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         MCPServerConfig
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid",
			cfg:     MCPServerConfig{URL: "https://example.com/mcp"},
			wantErr: false,
		},
		{
			name:        "empty URL rejected",
			cfg:         MCPServerConfig{URL: "   "},
			wantErr:     true,
			errContains: "URL",
		},
		{
			name:        "negative timeout rejected",
			cfg:         MCPServerConfig{URL: "https://example.com/mcp", Timeout: -1},
			wantErr:     true,
			errContains: "TIMEOUT",
		},
		{
			name:    "zero timeout valid",
			cfg:     MCPServerConfig{URL: "https://example.com/mcp", Timeout: 0},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.validate("server")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestConfig_ValidateBounds_MCPServerTimeout pins that the defensive
// bounds guard rejects a negative MCP server timeout at startup.
func TestConfig_ValidateBounds_MCPServerTimeout(t *testing.T) {
	t.Parallel()

	cfg := &Config{MCPServers: map[string]MCPServerConfig{
		"bad": {URL: "https://example.com/mcp", Timeout: -1},
	}}
	err := cfg.ValidateBounds()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP_SERVERS.bad.TIMEOUT")
	assert.Contains(t, err.Error(), "-1")
}

// TestConfig_ValidateBounds_MCPServerTimeoutValid is the negative control:
// a zero (default) MCP timeout must pass the bounds guard.
func TestConfig_ValidateBounds_MCPServerTimeoutValid(t *testing.T) {
	t.Parallel()

	cfg := &Config{MCPServers: map[string]MCPServerConfig{
		"ok": {URL: "https://example.com/mcp", Timeout: 0},
	}}
	require.NoError(t, cfg.ValidateBounds())
}

// TestConfig_UnmarshalYAML_MCPServers pins the yaml:"MCP_SERVERS" binding:
// a full YAML configuration block unmarshals into Config.MCPServers with
// the correct per-server fields and tri-state consent semantics.
func TestConfig_UnmarshalYAML_MCPServers(t *testing.T) {
	t.Parallel()

	input := `
MCP_SERVERS:
  github:
    URL: "https://api.githubcopilot.com/mcp/readonly"
    TOKEN: "ghs_secret"
    TIMEOUT: 120
  github-copilot:
    URL: "https://api.githubcopilot.com/mcp/"
    REQUIRES_CONSENT: true
`

	var cfg Config
	err := yaml.Unmarshal([]byte(input), &cfg)
	require.NoError(t, err)
	require.Len(t, cfg.MCPServers, 2)

	gh, ok := cfg.MCPServers["github"]
	require.True(t, ok, "github server must be bound")
	assert.Equal(t, "https://api.githubcopilot.com/mcp/readonly", gh.URL)
	assert.Equal(t, "ghs_secret", gh.Token)
	assert.Equal(t, 120, gh.Timeout)
	assert.Nil(t, gh.RequiresConsent, "unset REQUIRES_CONSENT must stay nil")
	assert.True(t, gh.IsReadOnly())
	assert.False(t, gh.EffectiveRequiresConsent())
	assert.False(t, gh.EffectiveSerial())

	gc, ok := cfg.MCPServers["github-copilot"]
	require.True(t, ok, "github-copilot server must be bound")
	assert.Equal(t, "https://api.githubcopilot.com/mcp/", gc.URL)
	require.NotNil(t, gc.RequiresConsent)
	assert.True(t, *gc.RequiresConsent)
	assert.True(t, gc.EffectiveRequiresConsent())
	assert.True(t, gc.EffectiveSerial())
}
