// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/cli/clitest"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingReader is a deterministic io.Reader fault stub. When n is zero, Read
// returns (0, err). When n is non-zero (always paired with a non-nil err in
// these tests), Read fills n bytes and returns (n, err), modelling the
// data-then-error pattern that io.ReadAll still surfaces as an error.
type failingReader struct {
	n   int
	err error
}

func (r failingReader) Read(p []byte) (int, error) {
	if r.n > 0 && len(p) > 0 {
		n := r.n
		if n > len(p) {
			n = len(p)
		}
		for i := 0; i < n; i++ {
			p[i] = 'x'
		}
		return n, r.err
	}
	return 0, r.err
}

// userInteractorOnly implements ONLY domain_security.UserInteractor. It must
// NOT implement ports.Capturer / ports.CapturerInteractor (no CapturePrompt,
// IsTTY, ReadLine, or Close), so the type assertions in setupCapturer
// (non-TUI path) and buildTUICapturer (TUI path) fail deterministically when
// newCapturer returns it.
type userInteractorOnly struct{}

var _ domain_security.UserInteractor = userInteractorOnly{}

func (userInteractorOnly) Confirm(_ stdctx.Context, _ string) (bool, error) { return false, nil }
func (userInteractorOnly) Warn(_ string)                                    {}
func (userInteractorOnly) Prompt(_ string)                                  {}
func (userInteractorOnly) ReadSingleKey(_ stdctx.Context) (string, error)   { return "", nil }

// TestResolveAndValidateCallbackID_DefensiveBranch covers the defensive-guard
// regex rejection in resolveAndValidateCallbackID (chat_command.go:459-468)
// reached only when c.cmd == nil. The identical regex check in the idChanged
// branch (line 453) is already covered by TestCallbackPreflight_CallbackID.
func TestResolveAndValidateCallbackID_DefensiveBranch(t *testing.T) {
	t.Parallel()

	c := &chatCommand{Stderr: io.Discard} // cmd nil → defensive guard path

	tests := []struct {
		name    string
		rawID   string
		wantID  string
		wantRe  *regexp.Regexp
		wantErr string
	}{
		{
			name:    "non-empty invalid id is rejected",
			rawID:   "id@bad",
			wantErr: "invalid --callback-id",
		},
		{
			name:   "non-empty valid id is returned unchanged",
			rawID:  "worker-1:42.a_b",
			wantID: "worker-1:42.a_b",
		},
		{
			name:   "empty id generates session id",
			rawID:  "",
			wantRe: regexp.MustCompile(`^session-[a-f0-9]{16}$`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveAndValidateCallbackID(c, tt.rawID)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.wantRe != nil {
				assert.Regexp(t, tt.wantRe, got)
				return
			}
			assert.Equal(t, tt.wantID, got)
		})
	}
}

// TestCaptureCallbackPrompt_StdinReadError covers the io.ReadAll stdin error
// branch in captureCallbackPrompt (chat_command.go:491-495): args are nil so
// the prompt is empty and the stdin path is taken.
func TestCaptureCallbackPrompt_StdinReadError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		r    io.Reader
	}{
		{
			name: "immediate read error",
			r:    failingReader{err: errors.New("read: injected failure")},
		},
		{
			name: "data then read error",
			r:    failingReader{n: 5, err: errors.New("read: injected failure")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &chatCommand{Stdin: tt.r, Stderr: io.Discard}
			prompt, err := captureCallbackPrompt(c, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "failed to read prompt from stdin")
			assert.Empty(t, prompt)
		})
	}
}

// TestCallbackExecution_EarlyACKWriteError covers the early-ACK
// fmt.Fprintf(c.Stdout, ...) error branch in executeCallbackWorkflow
// (chat_command.go:180-183). Preflight never writes stdout, so only the ACK
// write hits the failing writer — deterministic.
func TestCallbackExecution_EarlyACKWriteError(t *testing.T) {
	t.Parallel()

	var stderr strings.Builder
	// failingWriter is reused from cmd_env_test.go: it returns (0, err) on
	// every Write, so the early-ACK Fprintf is the only write to fail.
	cmdCtx, _ := setupPreflightTestContext(strings.NewReader(""), &failingWriter{err: errors.New("write: broken pipe")}, &stderr, nil, nil)

	err := executeChatCommand(cmdCtx, []string{"--callback", "https://example.com/webhook", "prompt"})
	require.Error(t, err)
	assert.Contains(t, stderr.String(), "failed to write ACK to stdout")
}

// TestExtractLastModelResponse_HistoryErrorsReturnEmpty covers every
// defensive "return empty string" branch in extractLastModelResponse
// (chat_command.go:281-290): nil bootstrapper, history-manager error / nil
// manager, GetLastModelTurn error / nil content, and thought-only content.
func TestExtractLastModelResponse_HistoryErrorsReturnEmpty(t *testing.T) {
	t.Parallel()

	historyErr := errors.New("history unavailable")

	newHistoryCommand := func(getTurn func(stdctx.Context) (int, *llm.Content, error)) *chatCommand {
		return &chatCommand{
			Stderr: io.Discard,
			Bootstrapper: &clitest.MockBootstrapper{
				GetHistoryManagerFunc: func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
					return &mockHistoryManagerWithTurn{getLastModelTurnFunc: getTurn}, nil
				},
			},
		}
	}

	tests := []struct {
		name string
		c    *chatCommand
		want string
	}{
		{
			name: "nil bootstrapper returns empty",
			c:    &chatCommand{Stderr: io.Discard},
			want: "",
		},
		{
			name: "history manager error returns empty",
			c: &chatCommand{
				Stderr: io.Discard,
				Bootstrapper: &clitest.MockBootstrapper{
					GetHistoryManagerFunc: func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
						return nil, historyErr
					},
				},
			},
			want: "",
		},
		{
			name: "nil history manager returns empty",
			c: &chatCommand{
				Stderr: io.Discard,
				Bootstrapper: &clitest.MockBootstrapper{
					GetHistoryManagerFunc: func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
						return nil, nil
					},
				},
			},
			want: "",
		},
		{
			name: "GetLastModelTurn error returns empty",
			c: newHistoryCommand(func(ctx stdctx.Context) (int, *llm.Content, error) {
				return 0, nil, historyErr
			}),
			want: "",
		},
		{
			name: "GetLastModelTurn nil content returns empty",
			c: newHistoryCommand(func(ctx stdctx.Context) (int, *llm.Content, error) {
				return 0, nil, nil
			}),
			want: "",
		},
		{
			name: "thought-only content returns empty",
			c: newHistoryCommand(func(ctx stdctx.Context) (int, *llm.Content, error) {
				return 0, &llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{Text: "chain", IsThought: true}},
				}, nil
			}),
			want: "",
		},
		{
			name: "mixed content returns non-thought text",
			c: newHistoryCommand(func(ctx stdctx.Context) (int, *llm.Content, error) {
				return 0, &llm.Content{
					Role: "model",
					Parts: []*llm.Part{
						{Text: "t", IsThought: true},
						{Text: "answer", IsThought: false},
					},
				}, nil
			}),
			want: "answer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.extractLastModelResponse(stdctx.Background(), &config.Config{})
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestExecuteInference_SetupChatSessionError covers the setupChatSession error
// propagation in executeInference (chat_command.go:235-237). The unexported
// capturerFactory injection returns a userInteractorOnly value that is NOT a
// ports.CapturerInteractor, so the type assertion in setupCapturer (non-TUI)
// or buildTUICapturer (TUI) fails and executeInference returns the error
// before any deferred cleanup or renderer use.
func TestExecuteInference_SetupChatSessionError(t *testing.T) {
	t.Parallel()

	factory := func(io.Reader, io.Writer, io.Writer, domain_security.Manager, clock.Clock, string, string, bool) domain_security.UserInteractor {
		return userInteractorOnly{}
	}

	tests := []struct {
		name       string
		opts       *cliOptions
		c          *chatCommand
		wantErrMsg string
	}{
		{
			name: "non-tui capturer assertion fails",
			opts: &cliOptions{},
			c: &chatCommand{
				Stderr:          io.Discard,
				capturerFactory: factory,
			},
			wantErrMsg: "did not return an ports.CapturerInteractor",
		},
		{
			name: "tui capturer assertion fails",
			opts: &cliOptions{tuiPrompt: true},
			c: &chatCommand{
				Stderr:          io.Discard,
				Bootstrapper:    &clitest.MockBootstrapper{},
				ChatService:     &clitest.MockChatService{},
				capturerFactory: factory,
			},
			wantErrMsg: "did not return a tui.BaseCapturer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preflight := &callbackPreflightResult{sessionID: "sess", prompt: "p"}
			err := tt.c.executeInference(stdctx.Background(), &config.Config{}, tt.opts, preflight)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}
