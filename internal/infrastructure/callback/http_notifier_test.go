// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package callback_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	domain_callback "github.com/gosharplite/tell-me-go/internal/domain/callback"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/callback"
)

func assertPayloadMatches(t *testing.T, body []byte, payload domain_callback.CallbackPayload, errStr string) {
	t.Helper()
	var receivedPayload domain_callback.CallbackPayload
	if err := json.Unmarshal(body, &receivedPayload); err != nil {
		t.Fatalf("failed to unmarshal request body: %v", err)
	}
	if receivedPayload.SessionID != payload.SessionID {
		t.Errorf("SessionID mismatch: got %q, want %q", receivedPayload.SessionID, payload.SessionID)
	}
	if receivedPayload.Status != payload.Status {
		t.Errorf("Status mismatch: got %q, want %q", receivedPayload.Status, payload.Status)
	}
	if receivedPayload.Error == nil || *receivedPayload.Error != errStr {
		t.Errorf("Error field mismatch: got %v, want %q", receivedPayload.Error, errStr)
	}
}

func assertReceivedRequest(t *testing.T, method string, header http.Header, body []byte, payload domain_callback.CallbackPayload, errStr string) {
	t.Helper()
	if method != http.MethodPost {
		t.Errorf("expected POST method, got: %q", method)
	}
	if ct := header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got: %q", ct)
	}
	if auth := header.Get("X-Custom-Auth"); auth != "Bearer token123" {
		t.Errorf("expected X-Custom-Auth 'Bearer token123', got: %q", auth)
	}
	if mode := header.Get("X-Request-Mode"); mode != "webhook" {
		t.Errorf("expected X-Request-Mode 'webhook', got: %q", mode)
	}
	assertPayloadMatches(t, body, payload, errStr)
}

func testSingleSuccess(t *testing.T, statusCode int) {
	t.Helper()
	var (
		mu          sync.Mutex
		gotMethod   string
		gotHeader   http.Header
		gotBodyData []byte
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotMethod = r.Method
		gotHeader = r.Header.Clone()
		gotBodyData, _ = io.ReadAll(r.Body)
		w.WriteHeader(statusCode)
	}))
	defer server.Close()

	notifier := callback.NewHTTPNotifierWithClient(server.Client())
	errStr := "test error message"
	payload := domain_callback.CallbackPayload{
		SessionID: "sess-123",
		Status:    domain_callback.StatusError,
		Response:  "",
		Error:     &errStr,
	}

	headers := map[string]string{
		"X-Custom-Auth":  "Bearer token123",
		"X-Request-Mode": "webhook",
	}

	if err := notifier.Notify(context.Background(), server.URL, headers, payload); err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	assertReceivedRequest(t, gotMethod, gotHeader, gotBodyData, payload, errStr)
}

func TestHTTPNotifier_Success(t *testing.T) {
	statusCodes := []int{http.StatusOK, http.StatusCreated, http.StatusNoContent}
	for _, statusCode := range statusCodes {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			testSingleSuccess(t, statusCode)
		})
	}
}

func TestHTTPNotifier_Non2xxError(t *testing.T) {
	statusCodes := []int{http.StatusBadRequest, http.StatusInternalServerError, http.StatusBadGateway}

	for _, statusCode := range statusCodes {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte("error response body"))
			}))
			defer server.Close()

			notifier := callback.NewHTTPNotifierWithClient(server.Client())
			payload := domain_callback.CallbackPayload{
				SessionID: "sess-fail",
				Status:    domain_callback.StatusSuccess,
				Response:  "output",
			}

			err := notifier.Notify(context.Background(), server.URL, nil, payload)
			if err == nil {
				t.Fatal("expected error for non-2xx status code, got nil")
			}
			statusCodeStr := string(rune('0' + statusCode/100))
			if !strings.Contains(err.Error(), statusCodeStr) {
				t.Errorf("expected error to contain status code %d, got: %v", statusCode, err)
			}
		})
	}
}

func isTimeoutError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "context") || strings.Contains(msg, "canceled") || strings.Contains(msg, "deadline")
}

func TestHTTPNotifier_ContextTimeout(t *testing.T) {
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-done:
		}
	}))
	defer func() {
		close(done)
		server.Close()
	}()

	notifier := callback.NewHTTPNotifierWithClient(server.Client())
	payload := domain_callback.CallbackPayload{
		SessionID: "sess-timeout",
		Status:    domain_callback.StatusSuccess,
		Response:  "timeout test",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := notifier.Notify(ctx, server.URL, nil, payload)
	if err == nil {
		t.Fatal("expected context timeout error, got nil")
	}
	if !isTimeoutError(err) {
		t.Errorf("expected error to indicate timeout/cancellation, got: %v", err)
	}
}

func TestHTTPNotifier_InvalidURL(t *testing.T) {
	notifier := callback.NewHTTPNotifier()
	payload := domain_callback.CallbackPayload{
		SessionID: "sess-badurl",
		Status:    domain_callback.StatusSuccess,
		Response:  "test",
	}

	ctx := context.Background()
	// A control character in URL causes http.NewRequestWithContext to fail
	err := notifier.Notify(ctx, "http://localhost:invalid\x7furl", nil, payload)
	if err == nil {
		t.Fatal("expected error with invalid URL, got nil")
	}
}

func TestNewHTTPNotifier_Constructors(t *testing.T) {
	notifier := callback.NewHTTPNotifier()
	if notifier == nil {
		t.Fatal("expected non-nil notifier from NewHTTPNotifier")
	}

	customClient := &http.Client{Timeout: 5 * time.Second}
	customNotifier := callback.NewHTTPNotifierWithClient(customClient)
	if customNotifier == nil {
		t.Fatal("expected non-nil notifier from NewHTTPNotifierWithClient")
	}
}
