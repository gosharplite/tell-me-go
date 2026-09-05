// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/cli/clitest"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/config/configtest"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPreflightTestContext(stdin io.Reader, stdout, stderr io.Writer, cfg *config.Config, modeLocker persistence.ModeLocker) (*context, *clitest.MockChatService) {
	if cfg == nil {
		cfg = &config.Config{
			Mode:               "assistant",
			BypassConfirmation: true,
		}
	}
	ml := &configtest.MockConfigLoader{
		LoadFunc: func(path string) (*config.Config, error) {
			return cfg, nil
		},
	}
	ms := &clitest.MockChatService{}
	mb := &clitest.MockBootstrapper{}
	if modeLocker == nil {
		modeLocker = &clitest.MockModeLocker{
			TryLockModeFunc: func(mode string) (func(), error) {
				return func() {}, nil
			},
		}
	}
	cmdCtx := &context{
		Version:          "1.0.0",
		Stdin:            stdin,
		Stdout:           stdout,
		Stderr:           stderr,
		SM:               &mockSM{},
		ChatService:      ms,
		Bootstrapper:     mb,
		Loader:           ml,
		ModeLocker:       modeLocker,
		CallbackNotifier: &clitest.MockCallbackNotifier{},
	}
	return cmdCtx, ms
}

func TestCallbackPreflight_MissingBypass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		bypassConfirmation bool
		expectErr          bool
	}{
		{
			name:               "bypass confirmation false fails fast",
			bypassConfirmation: false,
			expectErr:          true,
		},
		{
			name:               "bypass confirmation true succeeds",
			bypassConfirmation: true,
			expectErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			cfg := &config.Config{
				Mode:               "assistant",
				BypassConfirmation: tt.bypassConfirmation,
			}
			cmdCtx, _ := setupPreflightTestContext(strings.NewReader(""), &stdout, &stderr, cfg, nil)

			args := []string{"--callback", "https://example.com/webhook", "test prompt"}
			err := executeChatCommand(cmdCtx, args)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, stderr.String(), "BYPASS_CONFIRMATION: true")
				assert.NotContains(t, stdout.String(), "ACK")
			} else {
				require.NoError(t, err)
				assert.Empty(t, stderr.String())
				assert.Contains(t, stdout.String(), "ACK ")
			}
		})
	}
}

func TestCallbackPreflight_DisallowedFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		extraArgs    []string
		expectedFlag string
	}{
		{
			name:         "disallow retry flag",
			extraArgs:    []string{"--retry"},
			expectedFlag: "retry",
		},
		{
			name:         "disallow lastN flag short",
			extraArgs:    []string{"-l", "1"},
			expectedFlag: "last",
		},
		{
			name:         "disallow backN flag short",
			extraArgs:    []string{"-b", "1"},
			expectedFlag: "back",
		},
		{
			name:         "disallow diagnostic flag",
			extraArgs:    []string{"-d"},
			expectedFlag: "diagnostic",
		},
		{
			name:         "disallow turns flag",
			extraArgs:    []string{"-t"},
			expectedFlag: "turns",
		},
		{
			name:         "disallow edit-last flag",
			extraArgs:    []string{"-e"},
			expectedFlag: "edit-last",
		},
		{
			name:         "disallow interactive flag",
			extraArgs:    []string{"-i"},
			expectedFlag: "interactive",
		},
		{
			name:         "disallow tui-output flag",
			extraArgs:    []string{"-o"},
			expectedFlag: "tui-output",
		},
		{
			name:         "disallow update-turn flag",
			extraArgs:    []string{"--update-turn", "replacement text"},
			expectedFlag: "update-turn",
		},
		{
			name:         "disallow raw flag",
			extraArgs:    []string{"-r"},
			expectedFlag: "raw",
		},
		{
			name:         "disallow json flag",
			extraArgs:    []string{"--json"},
			expectedFlag: "json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			cmdCtx, _ := setupPreflightTestContext(strings.NewReader(""), &stdout, &stderr, nil, nil)

			args := append([]string{"--callback", "https://example.com/webhook"}, tt.extraArgs...)
			args = append(args, "test prompt")

			err := executeChatCommand(cmdCtx, args)
			require.Error(t, err)
			assert.Contains(t, stderr.String(), fmt.Sprintf("flag --%s is not allowed in callback mode", tt.expectedFlag))
			assert.NotContains(t, stdout.String(), "ACK")
		})
	}
}

func TestCallbackPreflight_AllowedFlags(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	cmdCtx, _ := setupPreflightTestContext(strings.NewReader(""), &stdout, &stderr, nil, nil)

	args := []string{
		"-c", "configs/butler.yaml",
		"--new",
		"--callback", "https://example.com/webhook",
		"--callback-id", "custom-session-123",
		"--callback-header", "X-Request-ID: req-001",
		"--callback-header", "Authorization: Bearer my-secret-token",
		"hello world prompt",
	}

	err := executeChatCommand(cmdCtx, args)
	require.NoError(t, err)
	assert.Empty(t, stderr.String())
	assert.Contains(t, stdout.String(), "ACK custom-session-123\n")
}

func TestCallbackPreflight_InvalidURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{
			name: "ftp scheme disallowed",
			url:  "ftp://example.com/webhook",
		},
		{
			name: "file scheme disallowed",
			url:  "file:///etc/passwd",
		},
		{
			name: "gopher scheme disallowed",
			url:  "gopher://example.com",
		},
		{
			name: "missing host in http URL",
			url:  "http:///path",
		},
		{
			name: "missing host in https URL",
			url:  "https://",
		},
		{
			name: "malformed URL",
			url:  "://bad-url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			cmdCtx, _ := setupPreflightTestContext(strings.NewReader(""), &stdout, &stderr, nil, nil)

			args := []string{"--callback", tt.url, "test prompt"}
			err := executeChatCommand(cmdCtx, args)
			require.Error(t, err)
			assert.Contains(t, stderr.String(), "invalid callback URL")
			assert.NotContains(t, stdout.String(), "ACK")
		})
	}
}

func TestCallbackPreflight_InvalidHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		headers       []string
		expectedErr   string
		sensitiveTest bool
		sensitiveWord string
	}{
		{
			name:        "missing colon format",
			headers:     []string{"InvalidHeaderFormat"},
			expectedErr: "invalid callback header format",
		},
		{
			name:        "invalid RFC 7230 token space in name",
			headers:     []string{"Invalid Header: Value"},
			expectedErr: "must match RFC 7230 token format",
		},
		{
			name:        "invalid RFC 7230 token at symbol in name",
			headers:     []string{"Header@Name: Value"},
			expectedErr: "must match RFC 7230 token format",
		},
		{
			name:        "invalid RFC 7230 token empty name",
			headers:     []string{": Value"},
			expectedErr: "must match RFC 7230 token format",
		},
		{
			name:        "CR character in header value",
			headers:     []string{"X-Custom: val\rwith-cr"},
			expectedErr: "illegal CR or LF characters",
		},
		{
			name:        "LF character in header value",
			headers:     []string{"X-Custom: val\nwith-lf"},
			expectedErr: "illegal CR or LF characters",
		},
		{
			name:          "CR in sensitive authorization header masks bearer value in stderr",
			headers:       []string{"Authorization: Bearer super-secret-token\r"},
			expectedErr:   "illegal CR or LF characters",
			sensitiveTest: true,
			sensitiveWord: "super-secret-token",
		},
		{
			name:          "LF in sensitive api key header masks value in stderr",
			headers:       []string{"X-API-Key: my-secret-api-key\n"},
			expectedErr:   "illegal CR or LF characters",
			sensitiveTest: true,
			sensitiveWord: "my-secret-api-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			cmdCtx, _ := setupPreflightTestContext(strings.NewReader(""), &stdout, &stderr, nil, nil)

			args := []string{"--callback", "https://example.com/webhook"}
			for _, h := range tt.headers {
				args = append(args, "--callback-header", h)
			}
			args = append(args, "test prompt")

			err := executeChatCommand(cmdCtx, args)
			require.Error(t, err)
			assert.Contains(t, stderr.String(), tt.expectedErr)
			assert.NotContains(t, stdout.String(), "ACK")

			if tt.sensitiveTest {
				assert.NotContains(t, stderr.String(), tt.sensitiveWord, "sensitive secret must not leak in stderr")
				assert.Contains(t, stderr.String(), "***", "masked value must appear in stderr telemetry")
			}
		})
	}
}

func TestCallbackPreflight_CallbackID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		expectErr   bool
		expectedErr string
		validateID  func(t *testing.T, sessionID string)
	}{
		{
			name:        "present-but-empty callback-id fails",
			args:        []string{"--callback-id="},
			expectErr:   true,
			expectedErr: "--callback-id cannot be empty",
		},
		{
			name:        "callback-id with invalid character @ fails",
			args:        []string{"--callback-id", "id@bad"},
			expectErr:   true,
			expectedErr: "invalid --callback-id",
		},
		{
			name:        "callback-id with space fails",
			args:        []string{"--callback-id", "id with space"},
			expectErr:   true,
			expectedErr: "invalid --callback-id",
		},
		{
			name:        "callback-id with plus symbol fails (closed charset)",
			args:        []string{"--callback-id", "+plus-id"},
			expectErr:   true,
			expectedErr: "invalid --callback-id",
		},
		{
			name:        "callback-id exceeding 128 characters fails",
			args:        []string{"--callback-id", strings.Repeat("a", 129)},
			expectErr:   true,
			expectedErr: "invalid --callback-id",
		},
		{
			name:      "valid custom callback-id passes",
			args:      []string{"--callback-id", "worker-node-1:sub.id_42"},
			expectErr: false,
		},
		{
			name:      "absent callback-id generates session-hex ID",
			args:      nil,
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			cmdCtx, _ := setupPreflightTestContext(strings.NewReader(""), &stdout, &stderr, nil, nil)

			args := []string{"--callback", "https://example.com/webhook"}
			args = append(args, tt.args...)
			args = append(args, "test prompt")

			err := executeChatCommand(cmdCtx, args)
			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, stderr.String(), tt.expectedErr)
				assert.NotContains(t, stdout.String(), "ACK")
			} else {
				require.NoError(t, err)
				assert.Empty(t, stderr.String())
				assert.Contains(t, stdout.String(), "ACK ")
			}
		})
	}

	// Also directly verify validateCallbackPreflight ID generation behavior
	t.Run("direct absent flag generates valid session ID format", func(t *testing.T) {
		c := &chatCommand{
			ModeLocker: &clitest.MockModeLocker{
				TryLockModeFunc: func(mode string) (func(), error) {
					return func() {}, nil
				},
			},
			Stderr: io.Discard,
		}
		cfg := &config.Config{Mode: "assistant", BypassConfirmation: true}
		opts := &cliOptions{
			callbackURL: "https://example.com/webhook",
		}

		res, err := c.validateCallbackPreflight(stdctx.Background(), cfg, opts, []string{"valid prompt"})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Regexp(t, regexp.MustCompile(`^session-[a-f0-9]{16}$`), res.sessionID)
	})

	t.Run("direct custom valid ID passes", func(t *testing.T) {
		c := &chatCommand{
			ModeLocker: &clitest.MockModeLocker{
				TryLockModeFunc: func(mode string) (func(), error) {
					return func() {}, nil
				},
			},
			Stderr: io.Discard,
		}
		cfg := &config.Config{Mode: "assistant", BypassConfirmation: true}
		opts := &cliOptions{
			callbackURL: "https://example.com/webhook",
			callbackID:  "my-worker-id:42_test.run",
		}

		res, err := c.validateCallbackPreflight(stdctx.Background(), cfg, opts, []string{"valid prompt"})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.Equal(t, "my-worker-id:42_test.run", res.sessionID)
	})
}

func TestCallbackPreflight_ContendedLock(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	mockLocker := &clitest.MockModeLocker{
		TryLockModeFunc: func(mode string) (func(), error) {
			return nil, persistence.ErrModeLocked
		},
	}
	cmdCtx, _ := setupPreflightTestContext(strings.NewReader(""), &stdout, &stderr, nil, mockLocker)

	args := []string{"--callback", "https://example.com/webhook", "test prompt"}
	err := executeChatCommand(cmdCtx, args)
	require.Error(t, err)
	assert.Contains(t, stderr.String(), "failed to acquire mode lock")
	assert.NotContains(t, stdout.String(), "ACK")
}

func TestCallbackPreflight_EmptyPrompt(t *testing.T) {
	t.Parallel()

	t.Run("empty prompt from args and stdin fails and releases lock", func(t *testing.T) {
		var lockAcquired, lockReleased bool
		mockLocker := &clitest.MockModeLocker{
			TryLockModeFunc: func(mode string) (func(), error) {
				lockAcquired = true
				return func() {
					lockReleased = true
				}, nil
			},
		}

		var stdout, stderr strings.Builder
		cmdCtx, _ := setupPreflightTestContext(strings.NewReader(""), &stdout, &stderr, nil, mockLocker)

		args := []string{"--callback", "https://example.com/webhook"}
		err := executeChatCommand(cmdCtx, args)
		require.Error(t, err)
		assert.Contains(t, stderr.String(), "prompt cannot be empty")
		assert.NotContains(t, stdout.String(), "ACK")

		assert.True(t, lockAcquired, "mode lock must have been acquired in step 6")
		assert.True(t, lockReleased, "mode lock must be released when step 7 prompt check fails")
	})

	t.Run("whitespace-only prompt from args and stdin fails and releases lock", func(t *testing.T) {
		var lockAcquired, lockReleased bool
		mockLocker := &clitest.MockModeLocker{
			TryLockModeFunc: func(mode string) (func(), error) {
				lockAcquired = true
				return func() {
					lockReleased = true
				}, nil
			},
		}

		var stdout, stderr strings.Builder
		cmdCtx, _ := setupPreflightTestContext(strings.NewReader("   \n\t  "), &stdout, &stderr, nil, mockLocker)

		args := []string{"--callback", "https://example.com/webhook", "   "}
		err := executeChatCommand(cmdCtx, args)
		require.Error(t, err)
		assert.Contains(t, stderr.String(), "prompt cannot be empty")
		assert.NotContains(t, stdout.String(), "ACK")

		assert.True(t, lockAcquired, "mode lock must have been acquired in step 6")
		assert.True(t, lockReleased, "mode lock must be released when step 7 prompt check fails")
	})
}

func TestCallbackPreflight_ValidPrompt(t *testing.T) {
	t.Parallel()

	t.Run("reads prompt from CLI args", func(t *testing.T) {
		var capturedPrompt string
		c := &chatCommand{
			ModeLocker: &clitest.MockModeLocker{
				TryLockModeFunc: func(mode string) (func(), error) {
					return func() {}, nil
				},
			},
			Stderr: io.Discard,
		}
		cfg := &config.Config{Mode: "assistant", BypassConfirmation: true}
		opts := &cliOptions{callbackURL: "https://example.com/webhook"}

		res, err := c.validateCallbackPreflight(stdctx.Background(), cfg, opts, []string{"prompt", "from", "cli", "args"})
		require.NoError(t, err)
		require.NotNil(t, res)
		capturedPrompt = res.prompt
		assert.Equal(t, "prompt from cli args", capturedPrompt)
	})

	t.Run("reads prompt from stdin when args empty", func(t *testing.T) {
		var capturedPrompt string
		c := &chatCommand{
			Stdin: strings.NewReader("prompt from standard input\n"),
			ModeLocker: &clitest.MockModeLocker{
				TryLockModeFunc: func(mode string) (func(), error) {
					return func() {}, nil
				},
			},
			Stderr: io.Discard,
		}
		cfg := &config.Config{Mode: "assistant", BypassConfirmation: true}
		opts := &cliOptions{callbackURL: "https://example.com/webhook"}

		res, err := c.validateCallbackPreflight(stdctx.Background(), cfg, opts, []string{})
		require.NoError(t, err)
		require.NotNil(t, res)
		capturedPrompt = res.prompt
		assert.Equal(t, "prompt from standard input", capturedPrompt)
	})
}

func TestMaskSensitiveHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		header   string
		value    string
		expected string
	}{
		{
			name:     "Authorization with Bearer prefix",
			header:   "Authorization",
			value:    "Bearer secret-token-12345",
			expected: "Bearer ***",
		},
		{
			name:     "authorization lower-case with bearer prefix",
			header:   "authorization",
			value:    "bearer secret-token-12345",
			expected: "Bearer ***",
		},
		{
			name:     "Authorization without Bearer prefix",
			header:   "Authorization",
			value:    "Basic dXNlcjpwYXNz",
			expected: "***",
		},
		{
			name:     "X-Auth-Token header",
			header:   "X-Auth-Token",
			value:    "auth-token-xyz",
			expected: "***",
		},
		{
			name:     "Api-Key header",
			header:   "Api-Key",
			value:    "api-key-999",
			expected: "***",
		},
		{
			name:     "X-Secret-Key header",
			header:   "X-Secret-Key",
			value:    "my-secret",
			expected: "***",
		},
		{
			name:     "Cookie header",
			header:   "Cookie",
			value:    "session_id=abcdef",
			expected: "***",
		},
		{
			name:     "Password header",
			header:   "X-User-Password",
			value:    "p@ssw0rd",
			expected: "***",
		},
		{
			name:     "Credentials header",
			header:   "X-Client-Credentials",
			value:    "cred-token",
			expected: "***",
		},
		{
			name:     "Non-sensitive Content-Type header",
			header:   "Content-Type",
			value:    "application/json",
			expected: "application/json",
		},
		{
			name:     "Non-sensitive Accept header",
			header:   "Accept",
			value:    "text/html, application/xhtml+xml",
			expected: "text/html, application/xhtml+xml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskSensitiveHeader(tt.header, tt.value)
			assert.Equal(t, tt.expected, got)
		})
	}
}
