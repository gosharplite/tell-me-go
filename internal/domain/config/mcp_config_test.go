// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestConfig_ValidateMCPServers_KeyValidation pins the MCP server key
// format contract: keys must match ^[a-z0-9-]{1,24}$ (1 to 24 lowercase
// alphanumeric and hyphens). Uppercase, underscores, dots, the empty string,
// and keys longer than 24 characters are all rejected; the 24-character
// boundary is accepted.
func TestConfig_ValidateMCPServers_KeyValidation(t *testing.T) {
	t.Parallel()

	valid := []string{"github", "github-copilot", "server-1", "a", strings.Repeat("a", 24)}
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

	invalid := []string{"GitHub", "my_server", "server.name", "", strings.Repeat("a", 25)}
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
			assert.Equal(t, tt.wantReadOnly, tt.cfg.isReadOnly())
			assert.Equal(t, tt.wantConsent, tt.cfg.EffectiveRequiresConsent())
			assert.Equal(t, tt.wantSerial, tt.cfg.EffectiveSerial())
		})
	}
}

// TestMCPServerConfig_EffectiveTimeout pins timeout defaulting: zero falls
// back to defaultMCPTimeoutSeconds (300s); a positive value is honored.
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

// TestConfig_ValidateMCPServers_NegativeTimeout pins that a negative MCP
// server timeout is hard-rejected during config loading via
// ValidateMCPServers. The redundant duplicate of this check in
// ValidateBounds was removed in the MCP CC refactor (2026-08); this test
// preserves the startup-rejection guarantee at its canonical home.
func TestConfig_ValidateMCPServers_NegativeTimeout(t *testing.T) {
	t.Parallel()

	cfg := &Config{MCPServers: map[string]MCPServerConfig{
		"bad": {URL: "https://example.com/mcp", Timeout: -1},
	}}
	err := cfg.ValidateMCPServers()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP_SERVERS.bad.TIMEOUT")
	assert.Contains(t, err.Error(), "-1")
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
	assert.True(t, gh.isReadOnly())
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

// TestMCPServerConfig_EffectiveAuth pins the auth-mode defaulting and
// normalization contract: an empty Auth defaults to "auto"; otherwise the
// value is lowercased and whitespace-trimmed.
func TestMCPServerConfig_EffectiveAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth string
		want string
	}{
		{"empty defaults to auto", "", MCPAuthAuto},
		{"explicit auto", "auto", MCPAuthAuto},
		{"explicit gh", "gh", MCPAuthGH},
		{"explicit bearer", "bearer", MCPAuthBearer},
		{"explicit basic", "basic", MCPAuthBasic},
		{"explicit none", "none", MCPAuthNone},
		{"mixed case normalized", "Bearer", MCPAuthBearer},
		{"surrounding whitespace trimmed", "  gh  ", MCPAuthGH},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := MCPServerConfig{Auth: tt.auth}
			assert.Equal(t, tt.want, c.EffectiveAuth())
		})
	}
}

// TestMCPServerConfig_Validate_AuthModes pins the auth-mode validation
// contract: the canonical modes (including mixed case) are accepted; unknown
// modes are rejected. NOTE: "basic" was intentionally migrated from the
// invalid set to the valid set (issue #1389, C1 scope) — previously rejected
// as unknown, it is now a first-class auth mode requiring USERNAME + TOKEN.
func TestMCPServerConfig_Validate_AuthModes(t *testing.T) {
	t.Parallel()

	valid := []string{"auto", "gh", "bearer", "basic", "none", "Bearer", "GH", "NONE", " AUTO "}
	for _, auth := range valid {
		auth := auth
		t.Run("valid_"+auth, func(t *testing.T) {
			t.Parallel()
			cfg := MCPServerConfig{URL: "https://example.com/mcp", Auth: auth}
			if cfg.EffectiveAuth() == MCPAuthBearer {
				cfg.Token = "tok"
			}
			if cfg.EffectiveAuth() == MCPAuthBasic {
				cfg.Username = "user"
				cfg.Token = "tok"
			}
			require.NoError(t, cfg.validate("server"))
		})
	}

	invalid := []string{"oauth", "gh-token", "token"}
	for _, auth := range invalid {
		auth := auth
		t.Run("invalid_"+auth, func(t *testing.T) {
			t.Parallel()
			cfg := MCPServerConfig{URL: "https://example.com/mcp", Auth: auth}
			err := cfg.validate("server")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "AUTH")
		})
	}
}

// TestMCPServerConfig_Validate_BasicCredentialsRequired pins that AUTH:
// "basic" with an empty (or whitespace-only) USERNAME or TOKEN is
// hard-rejected during config validation; both must be non-empty.
func TestMCPServerConfig_Validate_BasicCredentialsRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		token    string
	}{
		{"empty username", "", "tok"},
		{"empty token", "user", ""},
		{"whitespace username", "   ", "tok"},
		{"whitespace token", "user", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := MCPServerConfig{URL: "https://example.com/mcp", Auth: "basic", Username: tt.username, Token: tt.token}
			err := cfg.validate("server")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "MCP_SERVERS.server.USERNAME and TOKEN must not be empty when AUTH is basic")
		})
	}

	// Non-empty basic credentials are valid.
	cfg := MCPServerConfig{URL: "https://example.com/mcp", Auth: "basic", Username: "user", Token: "tok"}
	require.NoError(t, cfg.validate("server"))
}

// TestMCPServerConfig_Validate_BearerTokenRequired pins that AUTH: "bearer"
// with an empty (or whitespace-only) TOKEN is hard-rejected during config
// validation.
func TestMCPServerConfig_Validate_BearerTokenRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
	}{
		{"empty token", ""},
		{"whitespace token", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := MCPServerConfig{URL: "https://example.com/mcp", Auth: "bearer", Token: tt.token}
			err := cfg.validate("server")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "MCP_SERVERS.server.TOKEN must not be empty when AUTH is bearer")
		})
	}

	// A non-empty bearer token is valid.
	cfg := MCPServerConfig{URL: "https://example.com/mcp", Auth: "bearer", Token: "tok"}
	require.NoError(t, cfg.validate("server"))
}

// TestConfig_UnmarshalYAML_MCPServers_AuthField pins the yaml:"AUTH" and
// yaml:"USERNAME" bindings: the AUTH field unmarshals from YAML and flows
// into EffectiveAuth; USERNAME binds for basic-auth servers.
func TestConfig_UnmarshalYAML_MCPServers_AuthField(t *testing.T) {
	t.Parallel()

	input := `
MCP_SERVERS:
  github:
    URL: "https://api.githubcopilot.com/mcp/readonly"
    AUTH: "gh"
  local:
    URL: "https://example.com/mcp"
    AUTH: "bearer"
    TOKEN: "explicit-token"
  rovo:
    URL: "https://mcp.atlassian.com/v1/mcp"
    AUTH: "basic"
    USERNAME: "user@example.com"
    TOKEN: "tok"
`

	var cfg Config
	err := yaml.Unmarshal([]byte(input), &cfg)
	require.NoError(t, err)
	require.Len(t, cfg.MCPServers, 3)

	gh, ok := cfg.MCPServers["github"]
	require.True(t, ok, "github server must be bound")
	assert.Equal(t, MCPAuthGH, gh.EffectiveAuth())

	local, ok := cfg.MCPServers["local"]
	require.True(t, ok, "local server must be bound")
	assert.Equal(t, MCPAuthBearer, local.EffectiveAuth())
	assert.Equal(t, "explicit-token", local.Token)

	rovo, ok := cfg.MCPServers["rovo"]
	require.True(t, ok, "rovo server must be bound")
	assert.Equal(t, MCPAuthBasic, rovo.EffectiveAuth())
	assert.Equal(t, "user@example.com", rovo.Username)
	assert.Equal(t, "tok", rovo.Token)
}

// TestMCPServerConfig_IsStdio pins the stdio predicate: a server is stdio
// iff COMMAND is set — whitespace-only COMMAND does not count.
func TestMCPServerConfig_IsStdio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		want    bool
	}{
		{"empty command is not stdio", "", false},
		{"whitespace-only command is not stdio", "   ", false},
		{"command set is stdio", "npx", true},
		{"command with surrounding whitespace is stdio", "  npx  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := MCPServerConfig{Command: tt.command}
			assert.Equal(t, tt.want, c.IsStdio())
		})
	}
}

// TestMCPServerConfig_Validate_StdioModeConflict pins the stdio/HTTP mode
// contract: COMMAND and URL are mutually exclusive, neither may be absent,
// and ARGS/DIR/ENV require COMMAND. The most specific error wins (each
// check returns immediately).
func TestMCPServerConfig_Validate_StdioModeConflict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         MCPServerConfig
		wantErr     bool
		errContains string
	}{
		{
			name:        "COMMAND and URL are mutually exclusive",
			cfg:         MCPServerConfig{Command: "npx", URL: "https://example.com/mcp"},
			wantErr:     true,
			errContains: "mutually exclusive",
		},
		{
			name:        "neither COMMAND nor URL",
			cfg:         MCPServerConfig{},
			wantErr:     true,
			errContains: "URL must not be empty",
		},
		{
			name:        "ARGS without COMMAND",
			cfg:         MCPServerConfig{URL: "https://example.com/mcp", Args: []string{"--foo"}},
			wantErr:     true,
			errContains: "ARGS/DIR/ENV require COMMAND",
		},
		{
			name:        "DIR without COMMAND",
			cfg:         MCPServerConfig{URL: "https://example.com/mcp", Dir: "/tmp"},
			wantErr:     true,
			errContains: "ARGS/DIR/ENV require COMMAND",
		},
		{
			name:        "ENV without COMMAND",
			cfg:         MCPServerConfig{URL: "https://example.com/mcp", Env: map[string]string{"FOO": "bar"}},
			wantErr:     true,
			errContains: "ARGS/DIR/ENV require COMMAND",
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

// TestMCPServerConfig_Validate_StdioAuth pins the stdio auth contract:
// bearer/basic are hard-rejected under COMMAND (stdio servers transmit no
// credentials) — the mode conflict is diagnosed before the positive
// credential rules — while auto/gh/none/empty remain valid and stray
// TOKEN/USERNAME are tolerated.
func TestMCPServerConfig_Validate_StdioAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         MCPServerConfig
		wantErr     bool
		errContains string
	}{
		{
			name:        "COMMAND with bearer rejected",
			cfg:         MCPServerConfig{Command: "npx", Auth: "bearer", Token: "tok"},
			wantErr:     true,
			errContains: "requires an HTTP endpoint",
		},
		{
			name:        "COMMAND with basic rejected even with valid credentials",
			cfg:         MCPServerConfig{Command: "npx", Auth: "basic", Username: "user", Token: "tok"},
			wantErr:     true,
			errContains: "requires an HTTP endpoint",
		},
		{
			name:        "COMMAND with bearer and empty token: mode conflict wins",
			cfg:         MCPServerConfig{Command: "npx", Auth: "bearer"},
			wantErr:     true,
			errContains: "requires an HTTP endpoint",
		},
		{
			name:    "COMMAND with auto valid",
			cfg:     MCPServerConfig{Command: "npx", Auth: "auto"},
			wantErr: false,
		},
		{
			name:    "COMMAND with gh valid",
			cfg:     MCPServerConfig{Command: "npx", Auth: "gh"},
			wantErr: false,
		},
		{
			name:    "COMMAND with none valid",
			cfg:     MCPServerConfig{Command: "npx", Auth: "none"},
			wantErr: false,
		},
		{
			name:    "COMMAND with empty auth valid",
			cfg:     MCPServerConfig{Command: "npx"},
			wantErr: false,
		},
		{
			name:    "COMMAND with gh and stray token valid",
			cfg:     MCPServerConfig{Command: "npx", Auth: "gh", Token: "stray"},
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
				// The mode-conflict diagnosis must win over the positive
				// credential rules (bearer/basic missing-credential errors).
				assert.NotContains(t, err.Error(), "must not be empty when AUTH")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMCPServerConfig_ConsentAndSerial_Stdio pins the stdio defaults for
// consent and serialization: a stdio server defaults to no consent (trusted
// local spawn) and serial execution (no URL → not read-only). The explicit
// REQUIRES_CONSENT override still wins; HTTP URL-class defaults are
// regression-pinned.
func TestMCPServerConfig_ConsentAndSerial_Stdio(t *testing.T) {
	t.Parallel()

	trueVal := true
	falseVal := false

	tests := []struct {
		name        string
		cfg         MCPServerConfig
		wantConsent bool
		wantSerial  bool
	}{
		{
			name:        "stdio default: no consent, serial",
			cfg:         MCPServerConfig{Command: "npx"},
			wantConsent: false,
			wantSerial:  true,
		},
		{
			name:        "stdio with explicit consent true",
			cfg:         MCPServerConfig{Command: "npx", RequiresConsent: &trueVal},
			wantConsent: true,
			wantSerial:  true,
		},
		{
			name:        "stdio with explicit consent false",
			cfg:         MCPServerConfig{Command: "npx", RequiresConsent: &falseVal},
			wantConsent: false,
			wantSerial:  true,
		},
		{
			name:        "HTTP readonly regression",
			cfg:         MCPServerConfig{URL: "https://api.githubcopilot.com/mcp/readonly"},
			wantConsent: false,
			wantSerial:  false,
		},
		{
			name:        "HTTP mutating regression",
			cfg:         MCPServerConfig{URL: "https://api.githubcopilot.com/mcp/"},
			wantConsent: true,
			wantSerial:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.wantConsent, tt.cfg.EffectiveRequiresConsent())
			assert.Equal(t, tt.wantSerial, tt.cfg.EffectiveSerial())
		})
	}
}

// TestConfig_ValidateMCPServers_Stdio pins that a valid stdio server
// config survives ValidateMCPServers: the key-regex check runs first and
// the per-server validate() accepts a COMMAND-only stdio configuration.
func TestConfig_ValidateMCPServers_Stdio(t *testing.T) {
	t.Parallel()

	cfg := &Config{MCPServers: map[string]MCPServerConfig{
		"local-fs": {
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
			Dir:     "/tmp",
			Env:     map[string]string{"FOO": "bar"},
		},
	}}
	require.NoError(t, cfg.ValidateMCPServers())
}

// TestMCPServerConfig_Validate_DualViolationOrderingPins pins the
// most-specific-error-wins ordering across dual violations with
// require.EqualError (exact full error string — complementary to the
// assert.Contains fragments in the table tests). Each input is mutually
// distinct, so no two pins can fire on one input:
//
//   - TIMEOUT-wins: TIMEOUT precedes the auth switch (bogus auth + negative
//     timeout → the TIMEOUT error, not the AUTH error).
//   - URL-empty-wins: URL-empty precedes the ARGS check (no URL, no COMMAND,
//     stray ARGS → the URL-empty error, not the ARGS/DIR/ENV error).
//   - conflict-wins: the stdio bearer/basic conflict precedes TIMEOUT
//     (COMMAND + bearer + negative timeout → the conflict error).
func TestMCPServerConfig_Validate_DualViolationOrderingPins(t *testing.T) {
	t.Parallel()

	t.Run("TIMEOUT_wins_over_auth_switch", func(t *testing.T) {
		t.Parallel()
		cfg := MCPServerConfig{URL: "https://example.com/mcp", Auth: "bogus", Timeout: -1}
		err := cfg.validate("server")
		require.EqualError(t, err, "MCP_SERVERS.server.TIMEOUT must be >= 0, got -1")
	})

	t.Run("URL_empty_wins_over_args_check", func(t *testing.T) {
		t.Parallel()
		cfg := MCPServerConfig{Args: []string{"--foo"}}
		err := cfg.validate("server")
		require.EqualError(t, err, "MCP_SERVERS.server.URL must not be empty (or set COMMAND for a stdio server)")
	})

	t.Run("stdio_conflict_wins_over_timeout", func(t *testing.T) {
		t.Parallel()
		cfg := MCPServerConfig{Command: "npx", Auth: "bearer", Timeout: -1}
		err := cfg.validate("server")
		require.EqualError(t, err, "MCP_SERVERS.server.AUTH bearer/basic requires an HTTP endpoint; stdio (COMMAND) servers transmit no credentials")
	})
}
