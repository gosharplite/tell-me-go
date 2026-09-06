// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	stdctx "context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/cli/clitest"
	domain_callback "github.com/gosharplite/tell-me-go/internal/domain/callback"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/config/configtest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/redirectwriter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testHTTPNotifier struct {
	client *http.Client
}

func (n *testHTTPNotifier) Notify(ctx stdctx.Context, callbackURL string, headers map[string]string, payload domain_callback.CallbackPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("callback endpoint returned status %d", resp.StatusCode)
	}
	return nil
}

type trackingCloserWriter struct {
	bytes.Buffer
	mu     sync.Mutex
	closed bool
}

func (w *trackingCloserWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, io.ErrClosedPipe
	}
	return w.Buffer.Write(p)
}

func (w *trackingCloserWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

type mockHistoryManagerWithTurn struct {
	stubHistoryManager
	getLastModelTurnFunc func(ctx stdctx.Context) (int, *llm.Content, error)
}

func (m *mockHistoryManagerWithTurn) GetLastModelTurn(ctx stdctx.Context) (int, *llm.Content, error) {
	if m.getLastModelTurnFunc != nil {
		return m.getLastModelTurnFunc(ctx)
	}
	return m.stubHistoryManager.GetLastModelTurn(ctx)
}

func setupExecutionTestContext(
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	cfg *config.Config,
	chatService ports.ChatService,
	hManager ports.HistoryManager,
	notifier domain_callback.CallbackNotifier,
) (*context, *clitest.MockBootstrapper) {
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
	mb := &clitest.MockBootstrapper{
		GetHistoryManagerFunc: func(ctx stdctx.Context, c *config.Config) (ports.HistoryManager, error) {
			return hManager, nil
		},
	}
	locker := &clitest.MockModeLocker{
		TryLockModeFunc: func(mode string) (func(), error) {
			return func() {}, nil
		},
	}
	cmdCtx := &context{
		Version:          "1.0.0",
		Stdin:            stdin,
		Stdout:           stdout,
		Stderr:           stderr,
		SM:               &mockSM{},
		ChatService:      chatService,
		Bootstrapper:     mb,
		Loader:           ml,
		ModeLocker:       locker,
		CallbackNotifier: notifier,
	}
	return cmdCtx, mb
}

func TestCallbackExecution_Success(t *testing.T) {
	t.Parallel()

	var receivedPayload domain_callback.CallbackPayload
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := &testHTTPNotifier{client: server.Client()}

	hManager := &mockHistoryManagerWithTurn{
		getLastModelTurnFunc: func(ctx stdctx.Context) (int, *llm.Content, error) {
			content := &llm.Content{
				Role: "model",
				Parts: []*llm.Part{
					{Text: "Thinking deeply...", IsThought: true},
					{Text: "The calculated answer is ", IsThought: false},
					{Text: "42.", IsThought: false},
				},
			}
			return 1, content, nil
		},
	}

	ms := &clitest.MockChatService{
		ProcessMessageFunc: func(ctx stdctx.Context, cfg *config.Config, cmd ports.ChatCommand, capturer ports.CapturerInteractor) error {
			return nil
		},
	}

	tw := &trackingCloserWriter{}
	rw := redirectwriter.New(tw)
	var stderr strings.Builder

	cmdCtx, _ := setupExecutionTestContext(strings.NewReader(""), rw, &stderr, nil, ms, hManager, notifier)

	args := []string{"--callback", server.URL, "what is the answer?"}
	err := executeChatCommand(cmdCtx, args)
	require.NoError(t, err)

	// Verify Early-ACK was written to underlying stdout before detach
	mu.Lock()
	sessionID := receivedPayload.SessionID
	mu.Unlock()

	require.NotEmpty(t, sessionID)
	assert.Equal(t, "ACK "+sessionID+"\n", tw.String())

	// Verify Detach() was called on stdout
	assert.True(t, tw.closed, "base stdout must be closed by Detach()")

	// Verify detached stdout absorbs trailing writes into io.Discard
	n, writeErr := rw.Write([]byte("trailing write after detach"))
	assert.NoError(t, writeErr)
	assert.Equal(t, 27, n)
	assert.Equal(t, "ACK "+sessionID+"\n", tw.String(), "trailing writes must not reach base stdout")

	// Verify webhook received correct success payload
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, domain_callback.StatusSuccess, receivedPayload.Status)
	assert.Equal(t, "The calculated answer is 42.", receivedPayload.Response)
	assert.Nil(t, receivedPayload.Error)
}

func TestCallbackExecution_InferenceError_Delivered2xx(t *testing.T) {
	t.Parallel()

	var receivedPayload domain_callback.CallbackPayload
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := &testHTTPNotifier{client: server.Client()}

	historyCalled := false
	hManager := &mockHistoryManagerWithTurn{
		getLastModelTurnFunc: func(ctx stdctx.Context) (int, *llm.Content, error) {
			historyCalled = true
			return 1, &llm.Content{
				Role: "model",
				Parts: []*llm.Part{
					{Text: "stale previous turn response", IsThought: false},
				},
			}, nil
		},
	}

	ms := &clitest.MockChatService{
		ProcessMessageFunc: func(ctx stdctx.Context, cfg *config.Config, cmd ports.ChatCommand, capturer ports.CapturerInteractor) error {
			return errors.New("model context overflow")
		},
	}

	tw := &trackingCloserWriter{}
	rw := redirectwriter.New(tw)
	var stderr strings.Builder

	cmdCtx, _ := setupExecutionTestContext(strings.NewReader(""), rw, &stderr, nil, ms, hManager, notifier)

	args := []string{"--callback", server.URL, "prompt that causes overflow"}
	err := executeChatCommand(cmdCtx, args)

	// Must return nil on 2xx delivery even if inference errored
	require.NoError(t, err)

	// Invariant A7: GetLastModelTurn must NOT be called on error
	assert.False(t, historyCalled, "GetLastModelTurn must never be called when inference returns error")

	// Verify webhook payload
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, domain_callback.StatusError, receivedPayload.Status)
	assert.Equal(t, "", receivedPayload.Response, "response must be strictly empty on error")
	require.NotNil(t, receivedPayload.Error)
	assert.Equal(t, "model context overflow", *receivedPayload.Error)
}

func TestCallbackExecution_DeliveryFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	}))
	defer server.Close()

	notifier := &testHTTPNotifier{client: server.Client()}

	ms := &clitest.MockChatService{
		ProcessMessageFunc: func(ctx stdctx.Context, cfg *config.Config, cmd ports.ChatCommand, capturer ports.CapturerInteractor) error {
			return nil
		},
	}

	tw := &trackingCloserWriter{}
	rw := redirectwriter.New(tw)
	var stderr strings.Builder

	cmdCtx, _ := setupExecutionTestContext(strings.NewReader(""), rw, &stderr, nil, ms, nil, notifier)

	args := []string{"--callback", server.URL, "test prompt"}
	err := executeChatCommand(cmdCtx, args)

	// Delivery failure must return non-nil error (exit 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.Contains(t, stderr.String(), "callback delivery failed")
}

func TestCallbackExecution_Panic(t *testing.T) {
	t.Parallel()

	var receivedPayload domain_callback.CallbackPayload
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := &testHTTPNotifier{client: server.Client()}

	const panicMessage = "unexpected database crash"
	ms := &clitest.MockChatService{
		ProcessMessageFunc: func(ctx stdctx.Context, cfg *config.Config, cmd ports.ChatCommand, capturer ports.CapturerInteractor) error {
			panic(panicMessage)
		},
	}

	tw := &trackingCloserWriter{}
	rw := redirectwriter.New(tw)
	var stderr strings.Builder

	cmdCtx, _ := setupExecutionTestContext(strings.NewReader(""), rw, &stderr, nil, ms, nil, notifier)

	args := []string{"--callback", server.URL, "prompt that panics"}

	// Verify that executeChat re-panics with the original value to preserve runtime exit code 2
	assert.PanicsWithValue(t, panicMessage, func() {
		_ = executeChatCommand(cmdCtx, args)
	})

	// Verify that the webhook fired with status: "error" BEFORE the re-panic
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, domain_callback.StatusError, receivedPayload.Status)
	assert.Equal(t, "", receivedPayload.Response)
	require.NotNil(t, receivedPayload.Error)
	assert.Contains(t, *receivedPayload.Error, "panic: "+panicMessage)
}

func TestCallbackExecution_ContextCanceled(t *testing.T) {
	t.Parallel()

	var receivedPayload domain_callback.CallbackPayload
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := &testHTTPNotifier{client: server.Client()}

	ms := &clitest.MockChatService{
		ProcessMessageFunc: func(ctx stdctx.Context, cfg *config.Config, cmd ports.ChatCommand, capturer ports.CapturerInteractor) error {
			return ctx.Err()
		},
	}

	tw := &trackingCloserWriter{}
	rw := redirectwriter.New(tw)
	var stderr strings.Builder

	cmdCtx, _ := setupExecutionTestContext(strings.NewReader(""), rw, &stderr, nil, ms, nil, notifier)

	canceledCtx, cancel := stdctx.WithCancel(stdctx.Background())
	cancel() // pre-cancel context to simulate SIGTERM

	c := &chatCommand{
		Version:          cmdCtx.Version,
		Stdin:            cmdCtx.Stdin,
		Stdout:           cmdCtx.Stdout,
		Stderr:           cmdCtx.Stderr,
		SM:               cmdCtx.SM,
		ChatService:      cmdCtx.ChatService,
		Bootstrapper:     cmdCtx.Bootstrapper,
		Loader:           cmdCtx.Loader,
		ModeLocker:       cmdCtx.ModeLocker,
		CallbackNotifier: cmdCtx.CallbackNotifier,
	}

	opts := &cliOptions{
		callbackURL:    server.URL,
		updateTurnText: "__NOT_SET__",
	}

	err := c.executeChat(canceledCtx, opts, []string{"prompt on canceled ctx"})

	// Must return nil on 2xx delivery despite context cancellation
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, domain_callback.StatusError, receivedPayload.Status)
	assert.Equal(t, "", receivedPayload.Response)
	require.NotNil(t, receivedPayload.Error)
	assert.Equal(t, "context canceled", *receivedPayload.Error)
}

func TestCallbackExecution_CustomIDAndHeaders(t *testing.T) {
	t.Parallel()

	var receivedPayload domain_callback.CallbackPayload
	var receivedHeaders http.Header
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		receivedHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := &testHTTPNotifier{client: server.Client()}

	ms := &clitest.MockChatService{
		ProcessMessageFunc: func(ctx stdctx.Context, cfg *config.Config, cmd ports.ChatCommand, capturer ports.CapturerInteractor) error {
			return nil
		},
	}

	tw := &trackingCloserWriter{}
	rw := redirectwriter.New(tw)
	var stderr strings.Builder

	cmdCtx, _ := setupExecutionTestContext(strings.NewReader(""), rw, &stderr, nil, ms, nil, notifier)

	args := []string{
		"--callback", server.URL,
		"--callback-id", "custom-correlation-99",
		"--callback-header", "X-Trace-ID: trace-abc-123",
		"--callback-header", "Authorization: Bearer token-xyz-789",
		"prompt with custom headers",
	}

	err := executeChatCommand(cmdCtx, args)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "custom-correlation-99", receivedPayload.SessionID)
	assert.Equal(t, "trace-abc-123", receivedHeaders.Get("X-Trace-Id"))
	assert.Equal(t, "Bearer token-xyz-789", receivedHeaders.Get("Authorization"))
	assert.Equal(t, "application/json", receivedHeaders.Get("Content-Type"))
}
func TestCallbackExecution_PipeUnblocking_RealPipe(t *testing.T) {
	t.Parallel()

	rPipe, wPipe, err := os.Pipe()
	require.NoError(t, err)
	defer func() { _ = rPipe.Close() }()

	rw := redirectwriter.New(wPipe)

	inferenceStarted := make(chan struct{})
	inferenceRelease := make(chan struct{})

	ms := &clitest.MockChatService{
		ProcessMessageFunc: func(ctx stdctx.Context, cfg *config.Config, cmd ports.ChatCommand, capturer ports.CapturerInteractor) error {
			close(inferenceStarted)
			<-inferenceRelease
			return nil
		},
	}

	var receivedPayload domain_callback.CallbackPayload
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notifier := &testHTTPNotifier{client: server.Client()}

	var stderr strings.Builder
	cmdCtx, _ := setupExecutionTestContext(strings.NewReader(""), rw, &stderr, nil, ms, nil, notifier)

	execDone := make(chan error, 1)
	go func() {
		args := []string{"--callback", server.URL, "test pipe unblocking prompt"}
		execDone <- executeChatCommand(cmdCtx, args)
	}()

	// Wait until inference has started.
	// At this point, Early-ACK and Detach() must have completed.
	<-inferenceStarted

	// Read from rPipe while inference is STILL blocked.
	// If Detach() closed the underlying OS file descriptor, io.ReadAll will receive EOF
	// immediately and unblock, rather than hanging waiting for the process to exit.
	pipeOutput, readErr := io.ReadAll(rPipe)
	require.NoError(t, readErr)

	outStr := string(pipeOutput)
	assert.Regexp(t, `^ACK session-[0-9a-f]{16}\n$`, outStr)

	// Release inference now that pipe unblocking is proven
	close(inferenceRelease)

	err = <-execDone
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, domain_callback.StatusSuccess, receivedPayload.Status)
	expectedSessionID := strings.TrimPrefix(strings.TrimSuffix(outStr, "\n"), "ACK ")
	assert.Equal(t, expectedSessionID, receivedPayload.SessionID)
}
